package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/remote"
	"github.com/rickcrawford/tokenomics/internal/store"
	"github.com/spf13/cobra"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Start the central config server for remote token syncing",
	Long: `Runs a lightweight HTTP server that serves tokens over a REST API.
Proxy instances configured with a remote URL will fetch tokens from this server.
Clients can register webhook endpoints to receive push-based config updates.`,
	Example: `  tokenomics remote
  tokenomics remote --addr :9090 --api-key mysecret`,
	RunE: runRemote,
}

var (
	remoteAddr   string
	remoteAPIKey string
)

func init() {
	remoteCmd.Flags().StringVar(&remoteAddr, "addr", ":9090", "listen address (host:port)")
	remoteCmd.Flags().StringVar(&remoteAPIKey, "api-key", "", "API key for authenticating clients (optional)")
	rootCmd.AddCommand(remoteCmd)
}

func runRemote(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply --dir override if set
	if dirOverride != "" {
		cfg.Dir = dirOverride
		if !filepath.IsAbs(cfg.Dir) {
			if abs, err := filepath.Abs(cfg.Dir); err == nil {
				cfg.Dir = abs
			}
		}
		cfg.Storage.DBPath = filepath.Join(cfg.Dir, "tokenomics.db")
		if err := config.EnsureDir(cfg.Dir); err != nil {
			return fmt.Errorf("ensure directory: %w", err)
		}
	}

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	encKey := os.Getenv(cfg.Security.EncryptionKeyEnv)
	tokenStore := store.NewBoltStore(dbFile, encKey)
	if err := tokenStore.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer tokenStore.Close()
	tokenStore.StartFileWatch(5 * time.Second)

	// Init client registry for webhook client registrations
	clientsDBPath := filepath.Join(cfg.Dir, "clients.db")
	registry, err := remote.NewClientRegistry(clientsDBPath)
	if err != nil {
		return fmt.Errorf("init client registry: %w", err)
	}
	defer registry.Close()

	// Build combined emitter: static webhooks from config + dynamic registered clients
	staticEmitter := buildEmitter(cfg.Events)
	defer staticEmitter.Close()

	combinedEmitter := events.NewMulti(staticEmitter, registry)
	tokenStore.SetEmitter(combinedEmitter)

	clients, _ := registry.List()
	log.Printf("Client registry loaded: %d registered client(s)", len(clients))

	srv := remote.NewServer(tokenStore, remoteAPIKey, registry)

	httpServer := &http.Server{
		Addr:         remoteAddr,
		Handler:      srv,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Remote config server listening on %s", remoteAddr)
		if remoteAPIKey != "" {
			log.Println("API key authentication enabled")
		}
		log.Println("Client registration enabled on POST /api/v1/clients")
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("remote server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down remote server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
