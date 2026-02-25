package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
)

func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	resolved := pol.ResolveProvider("")

	// Look up provider config
	var providerCfg *config.ProviderConfig
	if resolved.ProviderName != "" {
		if pc, ok := h.providers[resolved.ProviderName]; ok {
			providerCfg = &pc
		}
	}

	// Resolve upstream: policy > provider config > global
	if resolved.UpstreamURL != "" {
		upstream = resolved.UpstreamURL
	} else if providerCfg != nil && providerCfg.UpstreamURL != "" {
		upstream = providerCfg.UpstreamURL
	}

	// Use provider's api_key_env if the resolved policy doesn't specify one
	if resolved.BaseKeyEnv == "" && providerCfg != nil && providerCfg.APIKeyEnv != "" {
		resolved.BaseKeyEnv = providerCfg.APIKeyEnv
	}

	logEntry := &RequestLog{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		TokenHash:   safePrefix(tokenHash, 16),
		BaseKeyEnv:  resolved.BaseKeyEnv,
		UpstreamURL: upstream,
		RemoteAddr:  r.RemoteAddr,
		UserAgent:   r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		if !h.logging.DisableRequest {
			logRequest(logEntry)
		}
	}()

	realKey := os.Getenv(resolved.BaseKeyEnv)
	if realKey == "" {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = fmt.Sprintf("base key env %q is not set", resolved.BaseKeyEnv)
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = "invalid upstream URL"
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	lw := newLoggingResponseWriter(w)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstreamURL.Scheme
			req.URL.Host = upstreamURL.Host
			req.Host = upstreamURL.Host

			// Remove Authorization header before setting provider-specific auth
			req.Header.Del("Authorization")

			// Apply auth based on provider scheme
			authScheme := "bearer"
			if providerCfg != nil && providerCfg.AuthScheme != "" {
				authScheme = providerCfg.AuthScheme
			}

			switch authScheme {
			case "header":
				authHeader := "Authorization"
				if providerCfg != nil && providerCfg.AuthHeader != "" {
					authHeader = providerCfg.AuthHeader
				}
				if authHeader != "" {
					req.Header.Set(authHeader, realKey)
				}
			case "query":
				q := req.URL.Query()
				q.Set("key", realKey)
				req.URL.RawQuery = q.Encode()
				req.Header.Del("Authorization")
			default:
				req.Header.Set("Authorization", "Bearer "+realKey)
			}

			// Add provider-specific extra headers
			if providerCfg != nil {
				for k, v := range providerCfg.Headers {
					req.Header.Set(k, v)
				}
			}
		},
	}

	proxy.ServeHTTP(lw, r)
	logEntry.StatusCode = lw.statusCode
}
