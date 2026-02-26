package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/proxy"
	"github.com/rickcrawford/tokenomics/internal/remote"
	"github.com/rickcrawford/tokenomics/internal/session"
	"github.com/rickcrawford/tokenomics/internal/store"
	tlsutil "github.com/rickcrawford/tokenomics/internal/tls"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the reverse proxy server",
	Example: `  tokenomics serve
  tokenomics serve --config /etc/tokenomics/config.yaml
  tokenomics serve --db /tmp/test.db`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// setupLogFile configures logging to write to a file (default: ~/.tokenomics/tokenomics.log)
// Can be overridden with TOKENOMICS_LOG_FILE env var or disabled with TOKENOMICS_LOG_STDOUT=1
func setupLogFile() error {
	// Check if user wants to disable file logging and use stdout instead
	if os.Getenv("TOKENOMICS_LOG_STDOUT") == "1" {
		// Use default stdout
		return nil
	}

	logFile := os.Getenv("TOKENOMICS_LOG_FILE")
	if logFile == "" {
		// Default to ~/.tokenomics/tokenomics.log
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home directory: %w", err)
		}
		logFile = filepath.Join(homeDir, ".tokenomics", "tokenomics.log")
	}

	// Create parent directory if needed
	dir := filepath.Dir(logFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Logging to %s", logFile)

	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	// Set up file logging
	if err := setupLogFile(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set up log file: %v\n", err)
		// Continue anyway, logs will go to stdout
	}


	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	// Init event emitter (early, so store can use it)
	emitter := buildEmitter(cfg.Events)
	defer emitter.Close()

	// Init token store (with optional at-rest encryption)
	encKey := os.Getenv(cfg.Security.EncryptionKeyEnv)
	tokenStore := store.NewBoltStore(dbFile, encKey)
	tokenStore.SetEmitter(emitter)
	if err := tokenStore.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer tokenStore.Close()
	tokenStore.StartFileWatch(5 * time.Second)

	// Remote sync (if configured)
	var remoteClient *remote.Client
	if cfg.Remote.URL != "" {
		remoteClient = buildRemoteClient(cfg.Remote)
		n, err := remoteClient.SyncTo(tokenStore)
		if err != nil {
			log.Printf("Remote sync failed: %v (continuing with local tokens)", err)
		} else {
			log.Printf("Remote sync: %d token(s) loaded from %s", n, cfg.Remote.URL)
		}
		if cfg.Remote.SyncSec > 0 {
			remoteClient.StartPeriodicSync(tokenStore, time.Duration(cfg.Remote.SyncSec)*time.Second)
			defer remoteClient.Stop()
		}
	}

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

	// Init session ledger (if enabled)
	var sessionLedger *ledger.Ledger
	if cfg.Ledger.Enabled {
		l, err := ledger.Open(cfg.Ledger.Dir, cfg.Ledger.Memory)
		if err != nil {
			log.Printf("Warning: could not open ledger: %v (continuing without ledger)", err)
		} else {
			sessionLedger = l
			defer func() {
				log.Printf("Ledger Close() called for session %s", l.SessionID())
				if err := sessionLedger.Close(); err != nil {
					log.Printf("Warning: ledger close error: %v", err)
				} else {
					log.Printf("Ledger closed successfully for session %s", l.SessionID())
				}
			}()
			log.Printf("Session ledger enabled (dir=%s, session=%s)", cfg.Ledger.Dir, l.SessionID())
		}
	}

	// Get hash key
	hashKey := getHashKey(cfg.Security.HashKeyEnv)

	// Initialize proxy logger with configured directory and file name.
	proxy.InitDebugLogger(cfg.Dir, cfg.Logging.ProxyLogFile)

	// Create proxy handler with provider configs and event emitter
	handler := proxy.NewHandler(tokenStore, sessStore, hashKey, cfg.Server.UpstreamURL, cfg.Providers, emitter)
	handler.SetLogging(cfg.Logging)
	handler.SetDebugLogDir(cfg.Dir) // Set the configured directory for debug logs
	if cfg.DefaultProvider != "" {
		handler.SetDefaultProvider(cfg.DefaultProvider)
	}
	if sessionLedger != nil {
		log.Printf("Setting ledger on handler (sessionID=%s)", sessionLedger.SessionID())
		handler.SetLedger(sessionLedger)
	} else {
		log.Printf("SessionLedger is nil, handler will not record to ledger")
	}

	// Wire up Redis memory writer if Redis session backend is configured
	if rs, ok := sessStore.(*session.RedisStore); ok {
		handler.SetRedisMemoryWriter(session.NewRedisMemoryWriter(rs.Client()))
	}

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

	// Webhook receiver for push-based token sync
	if cfg.Remote.Webhook.Enabled {
		receiver := remote.NewWebhookReceiver(cfg.Remote.Webhook, tokenStore, remoteClient)
		r.Post(receiver.Path(), receiver.ServeHTTP)
		log.Printf("Webhook receiver enabled on POST %s", receiver.Path())
	}

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

	// Emit server.start event (the "on load" event for key sync and readiness)
	emitter.Emit(context.Background(), events.New(events.ServerStart, map[string]interface{}{
		"http_port":  cfg.Server.HTTPPort,
		"https_port": cfg.Server.HTTPSPort,
		"tls":        cfg.Server.TLS.Enabled,
		"upstream":   cfg.Server.UpstreamURL,
	}))

	log.Println("Tokenomics proxy started. Press Ctrl+C to stop.")

	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errCh:
		return err
	}

	return nil
}

// buildRemoteClient creates a remote sync client from config.
func buildRemoteClient(cfg config.RemoteConfig) *remote.Client {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if cfg.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return remote.NewClient(cfg.URL, cfg.APIKey, httpClient)
}

// buildEmitter constructs the event emitter from config.
// Returns a Nop emitter if no webhooks are configured.
func buildEmitter(cfg config.EventsConfig) events.Emitter {
	if len(cfg.Webhooks) == 0 {
		return events.Nop{}
	}

	emitters := make([]events.Emitter, 0, len(cfg.Webhooks))
	for _, wh := range cfg.Webhooks {
		emitters = append(emitters, events.NewWebhookEmitter(events.WebhookConfig{
			URL:        wh.URL,
			Secret:     wh.Secret,
			SigningKey: wh.SigningKey,
			Events:     wh.Events,
			TimeoutSec: wh.TimeoutSec,
		}))
		log.Printf("Webhook registered: %s (events: %v)", wh.URL, wh.Events)
	}

	if len(emitters) == 1 {
		return emitters[0]
	}
	return events.NewMulti(emitters...)
}
