package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/tokencount"
)

// maxResponseBodySize limits upstream response body reads (32 MB).
const maxResponseBodySize = 32 * 1024 * 1024

// maxMemoryContentSize caps memory-captured content (512 KB).
const maxMemoryContentSize = 512 * 1024

// debugLogger is a persistent logger for proxy debug output
var (
	debugLogger      *log.Logger
	debugOnce        sync.Once
	debugLogDirPath  string // Set via InitDebugLogger before first use
	debugLogFileName string
)

// InitDebugLogger initializes the debug logger with the configured directory
func InitDebugLogger(dir, fileName string) {
	debugLogDirPath = dir
	debugLogFileName = fileName
	debugOnce.Do(initDebugLogger)
}

// debugLog writes debug information to a persistent logger
func debugLog(format string, args ...interface{}) {
	// If not initialized, just log to stdout via log package
	if debugLogger == nil && debugLogDirPath == "" {
		log.Printf("[debug] %s", fmt.Sprintf(format, args...))
		return
	}
	// Initialize on first use if directory was set
	if debugLogger == nil {
		debugOnce.Do(initDebugLogger)
	}
	if debugLogger != nil {
		debugLogger.Printf(format, args...)
	}
}

// initDebugLogger initializes the persistent debug logger using the configured directory
func initDebugLogger() {
	var logFile string

	// Use configured directory, or default to .tokenomics
	if debugLogDirPath == "" {
		debugLogDirPath = ".tokenomics"
	}
	if debugLogFileName == "" {
		debugLogFileName = "proxy.log"
	}

	// Ensure directory exists
	if err := os.MkdirAll(debugLogDirPath, 0o755); err == nil {
		logFile = filepath.Join(debugLogDirPath, debugLogFileName)
	} else {
		// Fallback to /tmp if directory can't be created
		logFile = "/tmp/tokenomics-proxy.log"
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	debugLogger = log.New(f, "", log.LstdFlags)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time, openClawMeta OpenClawMetadata) {
	// Extract OpenClaw metadata early so it's available in defer
	openClawEventMeta := openClawMetadataToMap(openClawMeta)

	logEntry := &RequestLog{
		Timestamp:  start.UTC().Format(time.RFC3339Nano),
		Method:     r.Method,
		Path:       r.URL.Path,
		TokenHash:  safePrefix(tokenHash, 16),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		Metadata:   openClawEventMeta,
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		if !h.logging.DisableRequest {
			logRequest(logEntry)
		}

		// Emit OpenClaw success/error events if metadata is present
		if len(openClawEventMeta) > 0 {
			if logEntry.StatusCode >= 200 && logEntry.StatusCode < 300 {
				h.emitOpenClawEvent(r.Context(), events.OpenClawAgentSuccess, openClawEventMeta, logEntry.StatusCode, logEntry.Model, tokenHash, "")
			} else {
				h.emitOpenClawEvent(r.Context(), events.OpenClawAgentError, openClawEventMeta, logEntry.StatusCode, logEntry.Model, tokenHash, logEntry.Error)
			}
		}
	}()

	// DEBUG: Log incoming request
	debugLog("=== Chat Completions Request ===")
	debugLog("Token Hash: %s, Upstream: %s, Path: %s", safePrefix(tokenHash, 16), upstream, r.URL.Path)

	// Read body (with size limit to prevent abuse)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		logEntry.StatusCode = http.StatusBadRequest
		logEntry.Error = "failed to read request body"
		httpError(w, http.StatusBadRequest, logEntry.Error)
		return
	}
	r.Body.Close()

	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		logEntry.StatusCode = http.StatusBadRequest
		logEntry.Error = "invalid JSON body"
		httpError(w, http.StatusBadRequest, logEntry.Error)
		return
	}

	// Extract model and resolve multi-provider policy
	model, _ := reqBody["model"].(string)
	logEntry.Model = model

	// Emit OpenClaw agent request event if metadata is present
	h.emitOpenClawEvent(r.Context(), events.OpenClawAgentRequest, openClawEventMeta, 0, model, tokenHash, "")

	// Log policy lookup to detailed logger
	debugLog("Policy loaded for token %s", safePrefix(tokenHash, 16))
	debugLog("BaseKeyEnv: %s", pol.BaseKeyEnv)

	resolved := pol.ResolveForModel(model)

	// Attach metadata to log entry (merge with OpenClaw metadata)
	if len(resolved.Metadata) > 0 {
		if logEntry.Metadata == nil {
			logEntry.Metadata = resolved.Metadata
		} else {
			// Merge resolved metadata into OpenClaw metadata
			for k, v := range resolved.Metadata {
				logEntry.Metadata[k] = v
			}
		}
	}

	// Resolve provider config for auth, headers, and chat path
	var providerCfg *config.ProviderConfig
	providerName := resolved.ProviderName

	// If no provider from routing, try policy's default provider
	if providerName == "" && pol.DefaultProvider != "" {
		providerName = pol.DefaultProvider
		debugLog("Using policy default provider: %s", providerName)
	}

	// If still no provider, try handler's default provider
	if providerName == "" && h.defaultProvider != "" {
		providerName = h.defaultProvider
		debugLog("Using handler default provider: %s", providerName)
	}

	if providerName != "" {
		if pc, ok := h.providers[providerName]; ok {
			providerCfg = &pc
		}
	}

	// Override upstream: policy > provider config > global
	if resolved.UpstreamURL != "" {
		upstream = resolved.UpstreamURL
	} else if providerCfg != nil && providerCfg.UpstreamURL != "" {
		upstream = providerCfg.UpstreamURL
	}

	// Use provider's api_key_env if the resolved policy doesn't specify one
	if resolved.BaseKeyEnv == "" && providerCfg != nil && providerCfg.APIKeyEnv != "" {
		resolved.BaseKeyEnv = providerCfg.APIKeyEnv
	}

	logEntry.BaseKeyEnv = resolved.BaseKeyEnv
	logEntry.UpstreamURL = upstream

	// Rate limit check
	if err := h.rateLimiter.Allow(tokenHash, resolved.RateLimit); err != nil {
		if emitErr := h.emitter.Emit(r.Context(), events.New(events.RateExceeded, map[string]interface{}{
			"token_hash": safePrefix(tokenHash, 16), "model": model, "error": err.Error(),
		})); emitErr != nil {
			debugLog("failed to emit rate exceeded event: %v", emitErr)
		}
		logEntry.StatusCode = http.StatusTooManyRequests
		logEntry.Error = err.Error()
		httpError(w, http.StatusTooManyRequests, err.Error())
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
		return
	}

	// Track parallel requests
	h.rateLimiter.Acquire(tokenHash, resolved.RateLimit)
	defer h.rateLimiter.Release(tokenHash, resolved.RateLimit)

	// Model check
	if err := resolved.CheckModel(model); err != nil {
		logEntry.StatusCode = http.StatusForbidden
		logEntry.Error = err.Error()
		httpError(w, http.StatusForbidden, err.Error())
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
		return
	}

	// Rules check on user messages (input scope)
	messages, _ := reqBody["messages"].([]interface{})
	for i, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}

		// Check rules — fail action returns an error, warn/log just return matches
		matches, err := resolved.CheckRules(content, "input")
		if err != nil {
			// Record the policy violation (fail) matches in structured log
			for _, match := range matches {
				logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
					Name:    match.Name,
					Action:  match.Action,
					Message: match.Message,
				})
				ruleEvent := map[string]interface{}{
					"token_hash": safePrefix(tokenHash, 16), "model": model,
					"rule_name": match.Name, "message": match.Message,
				}
				// Include OpenClaw metadata in rule violation events.
				for k, v := range openClawEventMeta {
					ruleEvent[k] = v
				}
				if emitErr := h.emitter.Emit(r.Context(), events.New(events.RuleViolation, ruleEvent)); emitErr != nil {
					debugLog("failed to emit rule violation event: %v", emitErr)
				}
			}
			log.Printf("[rule:fail] policy violation: %s (token=%s model=%s)", err.Error(), safePrefix(tokenHash, 16), model)
			logEntry.StatusCode = http.StatusForbidden
			logEntry.Error = err.Error()
			httpError(w, http.StatusForbidden, err.Error())
			h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
			return
		}

		// Record and log any warn/log matches
		for _, match := range matches {
			logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
				Name:    match.Name,
				Action:  match.Action,
				Message: match.Message,
			})
			evtType := events.RuleMatch
			if match.Action == "warn" {
				evtType = events.RuleWarning
			}
			if emitErr := h.emitter.Emit(r.Context(), events.New(evtType, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
				"rule_name": match.Name, "action": match.Action, "message": match.Message,
			})); emitErr != nil {
				debugLog("failed to emit rule event: %v", emitErr)
			}
			log.Printf("[rule:%s] %s (token=%s model=%s)", match.Action, match.Message, safePrefix(tokenHash, 16), model)
		}

		// Apply mask rules: redact content before forwarding
		masked := resolved.MaskContent(content, "input")
		if masked != content {
			msg["content"] = masked
			messages[i] = msg
			logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
				Action:  "mask",
				Message: "content redacted before forwarding",
			})
			if emitErr := h.emitter.Emit(r.Context(), events.New(events.RuleMask, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
			})); emitErr != nil {
				debugLog("failed to emit rule mask event: %v", emitErr)
			}
			log.Printf("[rule:mask] content redacted (token=%s model=%s)", safePrefix(tokenHash, 16), model)
		}
	}

	// Convert messages to typed slice for prompt injection
	typedMessages := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		if msg, ok := m.(map[string]interface{}); ok {
			typedMessages = append(typedMessages, msg)
		}
	}

	// Inject prompts from resolved policy
	typedMessages = resolved.InjectPrompts(typedMessages)

	// Count input tokens and check budget
	inputTokens, err := tokencount.CountMessages(model, typedMessages)
	if err != nil {
		log.Printf("[tokencount] error for model=%s token=%s: %v", model, safePrefix(tokenHash, 8), err)
		inputTokens = 0
	}
	logEntry.InputTokens = inputTokens

	var reservedInput int64
	var committedInput bool
	if resolved.MaxTokens > 0 {
		currentUsage, err := h.reserveInputBudget(tokenHash, int64(inputTokens), resolved.MaxTokens)
		if err != nil {
			if emitErr := h.emitter.Emit(r.Context(), events.New(events.BudgetExceeded, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
				"used": currentUsage, "input": inputTokens, "limit": resolved.MaxTokens,
			})); emitErr != nil {
				debugLog("failed to emit budget exceeded event: %v", emitErr)
			}
			logEntry.StatusCode = http.StatusTooManyRequests
			logEntry.Error = fmt.Sprintf("budget exceeded: used %d + input %d > limit %d", currentUsage, inputTokens, resolved.MaxTokens)
			httpError(w, http.StatusTooManyRequests, logEntry.Error)
			h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
			return
		}
		reservedInput = int64(inputTokens)
	}
	defer func() {
		if reservedInput > 0 && !committedInput {
			h.releaseReservedInputBudget(tokenHash, reservedInput)
		}
	}()

	// Update request body with injected messages
	interfaceMessages := make([]interface{}, len(typedMessages))
	for i, m := range typedMessages {
		interfaceMessages[i] = m
	}
	reqBody["messages"] = interfaceMessages

	// Check if streaming
	stream, _ := reqBody["stream"].(bool)
	logEntry.Stream = stream

	// Build the model list for retry/fallback: primary model first, then fallbacks
	modelsToTry := []string{model}
	if resolved.Retry != nil && len(resolved.Retry.Fallbacks) > 0 {
		modelsToTry = append(modelsToTry, resolved.Retry.Fallbacks...)
	}

	maxAttempts := 1
	if resolved.Retry != nil && resolved.Retry.MaxRetries > 0 {
		maxAttempts = 1 + resolved.Retry.MaxRetries
	}

	// Determine timeout
	timeout := 30 * time.Second // default
	if resolved.Timeout > 0 {
		timeout = time.Duration(resolved.Timeout) * time.Second
	}

	// Generate a client request ID for upstream correlation
	clientRequestID := generateRequestID()
	logEntry.ClientRequestID = clientRequestID

	// Try each model with retries
	var lastResp *http.Response
	var lastErr error
	var lastBody []byte
	retryCount := 0

	for _, tryModel := range modelsToTry {
		reqBody["model"] = tryModel

		for attempt := 0; attempt < maxAttempts; attempt++ {
			newBody, err := json.Marshal(reqBody)
			if err != nil {
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "failed to marshal request"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Compress request body if beneficial
			var requestEncoding string
			compressedBody, encoding, err := CompressRequestBody(newBody)
			if err == nil && encoding != "" {
				newBody = compressedBody
				requestEncoding = encoding
			}

			// Resolve the real API key from env
			debugLog("Loading env var: %s", resolved.BaseKeyEnv)
			realKey := os.Getenv(resolved.BaseKeyEnv)
			if realKey == "" {
				debugLog("ERROR: Env var %s not set", resolved.BaseKeyEnv)
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = fmt.Sprintf("base key env %q is not set", resolved.BaseKeyEnv)
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Build upstream request with timeout
			upstreamURL, err := url.Parse(upstream)
			if err != nil {
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "invalid upstream URL"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Use provider chat path if configured, otherwise use the original request path
			if providerCfg != nil && providerCfg.ChatPath != "" {
				upstreamURL.Path = providerCfg.ChatPath
			} else {
				upstreamURL.Path = r.URL.Path
			}
			upstreamURL.RawQuery = r.URL.RawQuery

			// Apply query-based auth if needed
			authScheme := "bearer"
			if providerCfg != nil && providerCfg.AuthScheme != "" {
				authScheme = providerCfg.AuthScheme
			}
			if authScheme == "query" {
				q := upstreamURL.Query()
				q.Set("key", realKey)
				upstreamURL.RawQuery = q.Encode()
			}

			debugLog("Upstream call: %s %s", r.Method, upstreamURL.String())

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), bytes.NewReader(newBody))
			if err != nil {
				cancel()
				debugLog("Error creating upstream request: %v", err)
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "failed to create upstream request"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Remove client's auth headers before copying, since we'll set provider-specific auth
			clientHeaders := r.Header.Clone()
			clientHeaders.Del("Authorization")
			clientHeaders.Del("x-api-key")
			copyHeaders(clientHeaders, proxyReq.Header)

			// Set auth based on provider scheme
			debugLog("Auth scheme: %s", authScheme)
			switch authScheme {
			case "header":
				authHeader := "Authorization"
				if providerCfg != nil && providerCfg.AuthHeader != "" {
					authHeader = providerCfg.AuthHeader
				}
				if authHeader != "" {
					debugLog("Setting header %s with real API key", authHeader)
					proxyReq.Header.Set(authHeader, realKey)
				}
			case "query":
				debugLog("Setting query parameter 'key' with real API key")
			default: // "bearer"
				debugLog("Setting Authorization: Bearer header with real API key")
				proxyReq.Header.Set("Authorization", "Bearer "+realKey)
			}

			// Add provider-specific extra headers
			if providerCfg != nil {
				for k, v := range providerCfg.Headers {
					proxyReq.Header.Set(k, v)
				}
			}

			proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
			if requestEncoding != "" {
				proxyReq.Header.Set("Content-Encoding", requestEncoding)
			}
			proxyReq.Header.Set("X-Client-Request-Id", clientRequestID)

			upstreamStart := time.Now()
			resp, err := h.client.Do(proxyReq)
			upstreamDuration := time.Since(upstreamStart)
			if err != nil {
				cancel()
				debugLog("Upstream request error: %v", err)
				lastErr = err
				retryCount++
				continue
			}
			debugLog("Upstream response: %d %s (duration: %dms)", resp.StatusCode, resp.Status, upstreamDuration.Milliseconds())

			// Check if we should retry based on status code
			if shouldRetry(resolved.Retry, resp.StatusCode) && (attempt < maxAttempts-1 || tryModel != modelsToTry[len(modelsToTry)-1]) {
				// Discard response body without allocating; cap size to prevent abuse
				if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBodySize)); err != nil {
					debugLog("failed to discard retry response body: %v", err)
				}
				resp.Body.Close()
				cancel()
				lastResp = resp
				retryCount++
				continue
			}

			// Success or non-retryable error — use this response
			cancel()
			logEntry.RetryCount = retryCount
			if tryModel != model {
				logEntry.FallbackModel = tryModel
			}

			// Capture upstream provider request ID from response headers
			logEntry.UpstreamRequestID = extractUpstreamRequestID(resp.Header)

			committedInput = true
			if emitErr := h.emitter.Emit(r.Context(), events.New(events.BudgetUpdate, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "input_tokens": inputTokens,
			})); emitErr != nil {
				debugLog("failed to emit budget update event: %v", emitErr)
			}

			// Record token usage for rate limiter
			h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, inputTokens)

			// Record full request body to memory and ledger (before any modifications)
			reqBodyStr := string(bodyBytes)
			if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
				if err := memWriter.Append(tokenHash, "request", model, reqBodyStr); err != nil {
					debugLog("Failed to record request to memory: %v", err)
				}
			}
			// Also record to ledger if enabled
			if h.ledger != nil {
				if err := h.ledger.RecordMemory(tokenHash, "request", model, reqBodyStr); err != nil {
					debugLog("Failed to record request to ledger: %v", err)
				}
			}

			// Collect normalized user content for memory logging.
			userContent := extractUserContent(messages)

			if stream && resp.StatusCode == http.StatusOK {
				outputTokens, assistantContent, streamUpstreamID, streamLastChunk, streamTruncated := h.handleStreamingResponse(w, resp, tokenHash, tryModel)
				resp.Body.Close()

				if streamTruncated {
					debugLog("Streaming assistant content capture truncated at %d bytes", maxMemoryContentSize)
				}
				logEntry.StatusCode = resp.StatusCode
				logEntry.OutputTokens = outputTokens
				logEntry.UpstreamID = streamUpstreamID
				h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, false)
				h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)
				if emitErr := h.emitter.Emit(r.Context(), events.New(events.RequestCompleted, map[string]interface{}{
					"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "stream": true,
					"status_code": resp.StatusCode, "input_tokens": inputTokens, "output_tokens": outputTokens,
				})); emitErr != nil {
					debugLog("failed to emit request completed event: %v", emitErr)
				}

				assistantForMemory := assistantContent
				if assistantForMemory == "" {
					assistantForMemory = "[Streaming response with no assistant content captured]"
				}
				if streamTruncated {
					assistantForMemory += "\n\n[assistant content truncated for memory]"
				}
				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					responseForMemory := assistantContent
					if responseForMemory == "" {
						responseForMemory = "[Streaming response with no assistant content captured]"
					} else if streamTruncated {
						responseForMemory += "\n\n[assistant content truncated for memory]"
					}
					if err := memWriter.Append(tokenHash, "response", tryModel, responseForMemory); err != nil {
						debugLog("Failed to record response to memory: %v", err)
					}
					if userContent != "" {
						if err := memWriter.Append(tokenHash, "user", tryModel, userContent); err != nil {
							debugLog("Failed to record user to memory: %v", err)
						}
					}
					if err := memWriter.Append(tokenHash, "assistant", tryModel, assistantForMemory); err != nil {
							debugLog("Failed to record assistant to memory: %v", err)
					}
				}

				// Record to ledger
				h.recordLedgerEntry(logEntry, tokenHash, tryModel, resolved.ProviderName,
					inputTokens, outputTokens, resp.StatusCode, stream, retryCount,
					extractProviderMetaFromStream(resp.Header, streamLastChunk))
				if h.ledger != nil {
					if userContent != "" {
						if err := h.ledger.RecordMemory(tokenHash, "user", tryModel, userContent); err != nil {
							debugLog("Failed to record user to ledger: %v", err)
						}
					}
					if err := h.ledger.RecordMemory(tokenHash, "assistant", tryModel, assistantForMemory); err != nil {
							debugLog("Failed to record assistant to ledger: %v", err)
					}
				}
				return
			}

			// Buffered response
			debugLog("Reading upstream response body (status=%d, content-length=%s)", resp.StatusCode, resp.Header.Get("Content-Length"))
			respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
			resp.Body.Close()
			if err != nil {
				logEntry.StatusCode = http.StatusBadGateway
				logEntry.Error = "failed to read upstream response"
				debugLog("ERROR reading upstream response: %v (bytes read: %d)", err, len(respBody))
				log.Printf("[error] Failed to read upstream response for %s:%s - %v", tryModel, resolved.ProviderName, err)
				httpError(w, http.StatusBadGateway, logEntry.Error)
				h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, 0, true)
				// Still record to ledger even on error
				h.recordLedgerEntry(logEntry, tokenHash, tryModel, resolved.ProviderName,
					inputTokens, 0, http.StatusBadGateway, stream, retryCount, nil)
				return
			}
			debugLog("Successfully read %d bytes from upstream response", len(respBody))

			outputTokens := h.countResponseTokens(respBody, tokenHash)
			logEntry.StatusCode = resp.StatusCode
			logEntry.OutputTokens = outputTokens
			logEntry.UpstreamID = extractUpstreamID(respBody)
			h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)

			isError := resp.StatusCode >= 400
			h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, isError)
			if emitErr := h.emitter.Emit(r.Context(), events.New(events.RequestCompleted, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "stream": false,
				"status_code": resp.StatusCode, "input_tokens": inputTokens, "output_tokens": outputTokens,
				"error": isError,
			})); emitErr != nil {
				debugLog("failed to emit request completed event: %v", emitErr)
			}

			var assistantContent string
			if !isError {
				// Record full response to memory (non-streaming)
				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					respFormatted := formatResponseForMemory(respBody, resp.Header.Get("Content-Type"))
					if err := memWriter.Append(tokenHash, "response", tryModel, respFormatted); err != nil {
						debugLog("Failed to record response to memory: %v", err)
					}
				}
				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					if userContent != "" {
						if err := memWriter.Append(tokenHash, "user", tryModel, userContent); err != nil {
							debugLog("Failed to record user to memory: %v", err)
						}
					}
					assistantContent = extractAssistantContent(respBody)
					if assistantContent == "" {
						assistantContent = formatResponseForMemory(respBody, resp.Header.Get("Content-Type"))
					}
					if err := memWriter.Append(tokenHash, "assistant", tryModel, assistantContent); err != nil {
						debugLog("Failed to record assistant to memory: %v", err)
					}
				}
			}

			// Record to ledger
			h.recordLedgerEntry(logEntry, tokenHash, tryModel, resolved.ProviderName,
				inputTokens, outputTokens, resp.StatusCode, stream, retryCount,
				extractProviderMeta(resp.Header, respBody))
			debugLog("About to record memory: ledger=%v, isError=%v, userContent=%d, assistantContent=%d",
				h.ledger != nil, isError, len(userContent), len(assistantContent))
			if h.ledger != nil && !isError {
				debugLog("Recording memory to ledger")
				if userContent != "" {
					debugLog("Recording user content: %d chars", len(userContent))
					if err := h.ledger.RecordMemory(tokenHash, "user", tryModel, userContent); err != nil {
						debugLog("Failed to record user to ledger: %v", err)
					}
				}
				if assistantContent == "" {
					assistantContent = extractAssistantContent(respBody)
				}
				if assistantContent != "" {
					debugLog("Recording assistant content: %d chars", len(assistantContent))
					if err := h.ledger.RecordMemory(tokenHash, "assistant", tryModel, assistantContent); err != nil {
						debugLog("Failed to record assistant to ledger: %v", err)
					}
					// Record request and response to ledger
					reqBodyStr := string(bodyBytes)
					if err := h.ledger.RecordMemory(tokenHash, "request", tryModel, reqBodyStr); err != nil {
						debugLog("Failed to record request to ledger: %v", err)
					}
					respFormatted := formatResponseForMemory(respBody, resp.Header.Get("Content-Type"))
					if err := h.ledger.RecordMemory(tokenHash, "response", tryModel, respFormatted); err != nil {
						debugLog("Failed to record response to ledger: %v", err)
					}
				}
			} else {
				debugLog("Skipping memory recording: ledger=%v, isError=%v", h.ledger != nil, isError)
			}

			copyHeaders(resp.Header, w.Header())
			w.WriteHeader(resp.StatusCode)
			if _, err := w.Write(respBody); err != nil {
				debugLog("failed writing buffered response to client: %v", err)
			}
			return
		}
	}

	// All retries/fallbacks exhausted
	logEntry.RetryCount = retryCount
	if lastResp != nil {
		logEntry.StatusCode = lastResp.StatusCode
		logEntry.Error = fmt.Sprintf("all retries exhausted (last status %d)", lastResp.StatusCode)
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
		copyHeaders(lastResp.Header, w.Header())
		w.WriteHeader(lastResp.StatusCode)
		if _, err := w.Write(lastBody); err != nil {
			debugLog("failed writing exhausted-retry response to client: %v", err)
		}
		return
	}
	logEntry.StatusCode = http.StatusBadGateway
	logEntry.Error = fmt.Sprintf("upstream request failed after %d attempts: %v", retryCount, lastErr)
	httpError(w, http.StatusBadGateway, logEntry.Error)
	h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
}

// shouldRetry checks if the status code should trigger a retry.
func shouldRetry(cfg *policy.RetryConfig, statusCode int) bool {
	if cfg == nil || cfg.MaxRetries == 0 {
		return false
	}
	retryOn := cfg.RetryOn
	if len(retryOn) == 0 {
		retryOn = []int{429, 500, 502, 503}
	}
	for _, code := range retryOn {
		if statusCode == code {
			return true
		}
	}
	return false
}

// handleStreamingResponse streams the upstream SSE response to the client,
// counting output tokens and extracting the upstream completion ID.
// Returns (outputTokens, assistantContent, upstreamID, lastChunk, truncated).
func (h *Handler) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, tokenHash, model string) (int, string, string, map[string]interface{}, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming not supported")
		return 0, "", "", nil, false
	}

	copyHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), maxResponseBodySize)
	var totalOutputTokens int
	var contentBuilder strings.Builder
	var upstreamID string
	var lastChunk map[string]interface{}
	var truncated bool

	for scanner.Scan() {
		line := scanner.Text()

		// Write the line through to the client
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		// Parse SSE data lines for token counting
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			lastChunk = chunk

			// Extract upstream ID from the first chunk
			if upstreamID == "" {
				if id, ok := chunk["id"].(string); ok {
					upstreamID = id
				} else if id, ok := chunk["responseId"].(string); ok {
					upstreamID = id
				}
			}

			// Count content tokens from delta
			if choices, ok := chunk["choices"].([]interface{}); ok {
				for _, c := range choices {
					choice, ok := c.(map[string]interface{})
					if !ok {
						continue
					}
					delta, ok := choice["delta"].(map[string]interface{})
					if !ok {
						continue
					}
					content, _ := delta["content"].(string)
					if content != "" {
						remaining := maxMemoryContentSize - contentBuilder.Len()
						if remaining > 0 {
							if len(content) > remaining {
								contentBuilder.WriteString(content[:remaining])
								truncated = true
							} else {
								contentBuilder.WriteString(content)
							}
						} else {
							truncated = true
						}
						n, err := tokencount.Count(model, content)
						if err == nil {
							totalOutputTokens += n
						}
					}
				}
			}

			// Also check usage field (some providers include it in stream chunks)
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				// Try Anthropic's "output_tokens" first
				if outputTokens, ok := usage["output_tokens"].(float64); ok {
					totalOutputTokens = int(outputTokens)
				} else if completionTokens, ok := usage["completion_tokens"].(float64); ok {
					// Fall back to OpenAI's "completion_tokens"
					totalOutputTokens = int(completionTokens)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		debugLog("stream scanner error: %v", err)
	}

	if totalOutputTokens > 0 {
		if _, err := h.sessions.AddUsage(tokenHash, int64(totalOutputTokens)); err != nil {
			debugLog("failed to add streaming usage: %v", err)
		}
	}

	return totalOutputTokens, contentBuilder.String(), upstreamID, lastChunk, truncated
}

func (h *Handler) countResponseTokens(body []byte, tokenHash string) int {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		debugLog("countResponseTokens: failed to unmarshal JSON: %v", err)
		return 0
	}

	// Debug: log what we got
	debugLog("countResponseTokens: response keys: %v", getKeys(resp))

	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		debugLog("countResponseTokens: no usage field in response, resp=%+v", resp)
		return 0
	}

	debugLog("countResponseTokens: usage field: %+v", usage)

	// Try multiple field names for output tokens (different providers use different names)
	var outputTokens float64

	// Anthropic uses "output_tokens" - could be float64 or int
	if ot, ok := usage["output_tokens"].(float64); ok {
		outputTokens = ot
		debugLog("countResponseTokens: found output_tokens as float64: %v", outputTokens)
	} else if ot, ok := usage["output_tokens"].(int); ok {
		outputTokens = float64(ot)
		debugLog("countResponseTokens: found output_tokens as int: %v", ot)
	} else if ct, ok := usage["completion_tokens"].(float64); ok {
		// OpenAI and others use "completion_tokens"
		outputTokens = ct
		debugLog("countResponseTokens: found completion_tokens as float64: %v", outputTokens)
	} else if ct, ok := usage["completion_tokens"].(int); ok {
		outputTokens = float64(ct)
		debugLog("countResponseTokens: found completion_tokens as int: %v", ct)
	} else {
		debugLog("countResponseTokens: no token field found in usage, usage keys: %v", getKeys(usage))
		return 0
	}

	debugLog("countResponseTokens: recording %d output tokens", int64(outputTokens))
	if outputTokens > 0 {
		if _, err := h.sessions.AddUsage(tokenHash, int64(outputTokens)); err != nil {
			debugLog("failed to add response usage: %v", err)
		}
	}
	return int(outputTokens)
}

// getKeys returns the keys of a map for debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// recordLedgerEntry records a request to the session ledger if configured.
func (h *Handler) recordLedgerEntry(logEntry *RequestLog, tokenHash, model, provider string,
	inputTokens, outputTokens, statusCode int, stream bool, retryCount int, providerMeta *ledger.ProviderMeta) {
	if h.ledger == nil {
		debugLog("recordLedgerEntry: ledger is nil, skipping")
		return
	}

	debugLog("recordLedgerEntry: recording request - model=%s, provider=%s, input=%d, output=%d, status=%d",
		model, provider, inputTokens, outputTokens, statusCode)

	entry := ledger.RequestEntry{
		Timestamp:         time.Now().UTC(),
		TokenHash:         tokenHash,
		Model:             model,
		Provider:          provider,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		DurationMs:        logEntry.DurationMs,
		StatusCode:        statusCode,
		Stream:            stream,
		Error:             logEntry.Error,
		UpstreamID:        logEntry.UpstreamID,
		UpstreamRequestID: logEntry.UpstreamRequestID,
		RetryCount:        retryCount,
		FallbackModel:     logEntry.FallbackModel,
		Metadata:          logEntry.Metadata,
		ProviderMeta:      providerMeta,
	}

	// Convert rule matches
	for _, rm := range logEntry.RuleMatches {
		entry.RuleMatches = append(entry.RuleMatches, ledger.RuleMatchEntry{
			Name:    rm.Name,
			Action:  rm.Action,
			Message: rm.Message,
		})
	}

	h.ledger.RecordRequest(entry)
}

func extractUserContent(messages []interface{}) string {
	var parts []string
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}

		switch content := msg["content"].(type) {
		case string:
			if content != "" {
				parts = append(parts, content)
			}
		case []interface{}:
			for _, item := range content {
				block, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if text, _ := block["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func extractAssistantContent(body []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}

	// Anthropic-style non-streaming response: {"content":[{"type":"text","text":"..."}], ...}
	if contentArr, ok := resp["content"].([]interface{}); ok && len(contentArr) > 0 {
		var b strings.Builder
		for _, item := range contentArr {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := block["text"].(string); text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return ""
	}
	msg, ok := choice["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
}

// decompressResponseBody attempts to decompress gzip-compressed response body.
// If the body is not gzipped, returns the original body.
func decompressResponseBody(body []byte, contentEncoding string) []byte {
	// Check if response is gzip-compressed
	if !strings.Contains(strings.ToLower(contentEncoding), "gzip") {
		return body
	}

	// Try to decompress
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		// Not actually gzipped despite header, return original
		return body
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(io.LimitReader(reader, maxResponseBodySize))
	if err != nil {
		// Decompression failed, return original
		return body
	}
	return decompressed
}

// formatResponseForMemory extracts and formats the response content as clean markdown.
// For JSON responses, it parses and extracts the assistant content.
// For other content types, returns a brief summary.
func formatResponseForMemory(body []byte, contentType string) string {
	// Try to parse as JSON (most API responses are JSON)
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		// For non-JSON payloads, persist the raw content as-is.
		return string(body)
	}

	// Extract common response fields
	var result strings.Builder

	// Check for error in response
	if errMsg, ok := resp["error"].(map[string]interface{}); ok {
		if msg, ok := errMsg["message"].(string); ok {
			result.WriteString(fmt.Sprintf("**Error:** %s\n\n", msg))
		}
	}

	// Extract assistant content if available
	if contentArr, ok := resp["content"].([]interface{}); ok && len(contentArr) > 0 {
		var b strings.Builder
		for _, item := range contentArr {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok && text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}

	// OpenAI-compatible content shape.
	if choices, ok := resp["choices"].([]interface{}); ok && len(choices) > 0 {
		choice := choices[0]
		if choiceMap, ok := choice.(map[string]interface{}); ok {
			if msg, ok := choiceMap["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					result.WriteString(content)
					return result.String()
				}
			}
			if delta, ok := choiceMap["delta"].(map[string]interface{}); ok {
				if content, ok := delta["content"].(string); ok {
					result.WriteString(content)
					return result.String()
				}
			}
		}
	}

	// If we got JSON but couldn't extract assistant text, keep a compact summary.
	result.WriteString(string(body))
	return result.String()
}

func (h *Handler) reserveInputBudget(tokenHash string, inputTokens, maxTokens int64) (int64, error) {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()

	currentUsage, err := h.sessions.GetUsage(tokenHash)
	if err != nil {
		return currentUsage, err
	}
	if currentUsage+inputTokens > maxTokens {
		return currentUsage, fmt.Errorf("budget exceeded")
	}
	if inputTokens > 0 {
		if _, err := h.sessions.AddUsage(tokenHash, inputTokens); err != nil {
			return currentUsage, err
		}
	}
	return currentUsage, nil
}

func (h *Handler) releaseReservedInputBudget(tokenHash string, reservedInput int64) {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	if reservedInput <= 0 {
		return
	}
	if _, err := h.sessions.AddUsage(tokenHash, -reservedInput); err != nil {
		debugLog("failed to release reserved input budget: %v", err)
	}
}

// emitOpenClawEvent emits webhook events for OpenClaw agent requests.
// Emits on agent request, success, error, or rule violations.
func (h *Handler) emitOpenClawEvent(ctx context.Context, eventType string, metadata map[string]string, statusCode int, model string, tokenHash string, errorMsg string) {
	if len(metadata) == 0 {
		return // No OpenClaw metadata, skip event
	}

	data := map[string]interface{}{
		"token_hash": safePrefix(tokenHash, 16),
		"model":      model,
		"status":     statusCode,
	}

	// Add all OpenClaw metadata fields to the event
	for k, v := range metadata {
		if v != "" {
			data[k] = v
		}
	}

	// Add error message if present
	if errorMsg != "" {
		data["error"] = errorMsg
	}

	if err := h.emitter.Emit(ctx, events.New(eventType, data)); err != nil {
		debugLog("failed to emit OpenClaw event %s: %v", eventType, err)
	}
}
