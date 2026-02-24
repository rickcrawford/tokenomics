package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/proxy"
	"github.com/rickcrawford/tokenomics/internal/session"
	"github.com/rickcrawford/tokenomics/internal/store"
	tlsutil "github.com/rickcrawford/tokenomics/internal/tls"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the reverse proxy server",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	// Init token store
	tokenStore := store.NewBoltStore(dbFile)
	if err := tokenStore.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer tokenStore.Close()
	tokenStore.StartFileWatch(5 * time.Second)

	// Init session store
	var sessStore session.Store
	switch cfg.Session.Backend {
	case "redis":
		sessStore = session.NewRedisStore(
			cfg.Session.Redis.Addr,
			cfg.Session.Redis.Password,
			cfg.Session.Redis.DB,
		)
	default:
		sessStore = session.NewMemoryStore()
	}

	// Get hash key
	hashKey := getHashKey(cfg.Security.HashKeyEnv)

	// Create proxy handler
	handler := proxy.NewHandler(tokenStore, sessStore, hashKey, cfg.Server.UpstreamURL)

	// Build chi router with common middleware
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Heartbeat("/ping"))

	// Management endpoints (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/stats", handler.Stats().StatsHandler)

	// All other routes go through the proxy handler
	r.Handle("/*", handler)

	// Setup graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)

	// HTTP server (optional, for health checks or non-TLS)
	if cfg.Server.HTTPPort > 0 {
		httpServer := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Server.HTTPPort),
			Handler: r,
		}
		go func() {
			log.Printf("HTTP server listening on :%d", cfg.Server.HTTPPort)
			if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
				errCh <- fmt.Errorf("http: %w", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			httpServer.Shutdown(shutdownCtx)
		}()
	}

	// HTTPS server
	if cfg.Server.TLS.Enabled {
		var certFile, keyFile string

		if cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
			certFile = cfg.Server.TLS.CertFile
			keyFile = cfg.Server.TLS.KeyFile
		} else if cfg.Server.TLS.AutoGen {
			certDir := cfg.Server.TLS.CertDir
			if certDir == "" {
				certDir = "./certs"
			}
			paths, err := tlsutil.EnsureCerts(certDir)
			if err != nil {
				return fmt.Errorf("auto-generate certs: %w", err)
			}
			certFile = paths.ServerCert
			keyFile = paths.ServerKey
			log.Printf("Using auto-generated certificates from %s", certDir)
			log.Printf("CA cert for trust installation: %s", paths.CACert)
		} else {
			return fmt.Errorf("TLS enabled but no cert files provided and auto_gen is disabled")
		}

		tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("load TLS cert: %w", err)
		}

		httpsServer := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Server.HTTPSPort),
			Handler: r,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
				MinVersion:   tls.VersionTLS12,
			},
		}

		go func() {
			log.Printf("HTTPS server listening on :%d", cfg.Server.HTTPSPort)
			if err := httpsServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				errCh <- fmt.Errorf("https: %w", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			httpsServer.Shutdown(shutdownCtx)
		}()
	}

	log.Println("Tokenomics proxy started. Press Ctrl+C to stop.")

	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errCh:
		return err
	}

	return nil
}
