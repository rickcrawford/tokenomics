package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
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

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Never write application logs to stdout/stderr.
	// If file logging is disabled, discard logs entirely.
	log.SetOutput(io.Discard)

	// Apply --dir override if set (takes precedence over config file)
	if dirOverride != "" {
		cfg.Dir = dirOverride
		// Make it absolute
		if !filepath.IsAbs(cfg.Dir) {
			if abs, err := filepath.Abs(cfg.Dir); err == nil {
				cfg.Dir = abs
			}
		}
	}

	// Set default db path if not already set
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = filepath.Join(cfg.Dir, "tokenomics.db")
	}

	// Ensure the main directory exists
	if err := config.EnsureDir(cfg.Dir); err != nil {
		return fmt.Errorf("ensure directory: %w", err)
	}

	// Set up file logging if enabled
	if cfg.Logging.File.Enabled {
		logPath := cfg.Logging.File.Path
		if logPath == "" {
			logPath = filepath.Join(cfg.Dir, "tokenomics.log")
		}
		// Create parent directory if needed
		logDir := filepath.Dir(logPath)
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("Logging to %s (level: %s)", logPath, cfg.Logging.File.Level)
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
	var registeredClientID string
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

		// Auto-register webhook with central server if configured
		if cfg.Remote.Webhook.AutoRegister && cfg.Remote.Webhook.CallbackURL != "" {
			reg := remote.ClientRegistration{
				URL:       cfg.Remote.Webhook.CallbackURL,
				Secret:    cfg.Remote.Webhook.Secret,
				SigningKey: cfg.Remote.Webhook.SigningKey,
				Events:    []string{"token.*"},
				Insecure:  cfg.Remote.Webhook.Insecure,
			}
			id, err := remoteClient.RegisterWebhook(reg)
			if err != nil {
				log.Printf("Webhook auto-registration failed: %v (push sync will not work)", err)
			} else {
				registeredClientID = id
				log.Printf("Webhook auto-registered with central server (client_id=%s, callback=%s)", id, cfg.Remote.Webhook.CallbackURL)
			}
		}
	}
	// Deferred cleanup: unregister webhook on shutdown if we registered
	if registeredClientID != "" {
		defer func() {
			if err := remoteClient.UnregisterWebhook(registeredClientID); err != nil {
				log.Printf("Webhook unregister failed: %v", err)
			} else {
				log.Printf("Webhook unregistered from central server (client_id=%s)", registeredClientID)
			}
		}()
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
		l, err := ledger.Open(cfg.Dir, cfg.Ledger.Memory)
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
			log.Printf("Session ledger enabled (dir=%s, session=%s)", cfg.Dir, l.SessionID())
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
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			log.Printf("health write error: %v", err)
		}
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
	var shutdownWG sync.WaitGroup

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
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("http shutdown error: %v", err)
			}
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
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpsServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("https shutdown error: %v", err)
			}
		}()
	}

	// Emit server.start event (the "on load" event for key sync and readiness)
	if err := emitter.Emit(context.Background(), events.New(events.ServerStart, map[string]interface{}{
		"http_port":  cfg.Server.HTTPPort,
		"https_port": cfg.Server.HTTPSPort,
		"tls":        cfg.Server.TLS.Enabled,
		"upstream":   cfg.Server.UpstreamURL,
	})); err != nil {
		log.Printf("server.start emit failed: %v", err)
	}

	log.Println("Tokenomics proxy started. Press Ctrl+C to stop.")

	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errCh:
		return err
	}

	// Wait for all HTTP servers to finish draining in-flight requests
	// before returning. Deferred resource cleanup (tokenStore.Close,
	// emitter.Close, etc.) runs after this, so the DB is only closed
	// once no handlers are still running.
	shutdownWG.Wait()
	log.Println("All servers stopped, releasing resources...")

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
			Insecure:   wh.Insecure,
		}))
		log.Printf("Webhook registered: %s (events: %v, insecure: %v)", wh.URL, wh.Events, wh.Insecure)
	}

	if len(emitters) == 1 {
		return emitters[0]
	}
	return events.NewMulti(emitters...)
}
