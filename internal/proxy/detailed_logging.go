package proxy

import (
	"fmt"
	"time"
)

// DetailedLogger writes detailed logs via the shared proxy logger.
type DetailedLogger struct {
}

// NewDetailedLogger creates a detailed logger using shared logging.
func NewDetailedLogger() *DetailedLogger {
	return &DetailedLogger{}
}

// NewDetailedLoggerWithDir creates a detailed logger.
// Directory is accepted for compatibility with existing call sites.
func NewDetailedLoggerWithDir(dir string) *DetailedLogger {
	return &DetailedLogger{}
}

// Log writes a message
func (dl *DetailedLogger) Log(msg string) {
	debugLog("[detailed] %s", msg)
}

// Logf writes a formatted message
func (dl *DetailedLogger) Logf(format string, args ...interface{}) {
	debugLog("[detailed] "+format, args...)
}

// maskValue masks sensitive values for logging
func maskValue(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

// LogTokenExtraction logs token extraction
func (dl *DetailedLogger) LogTokenExtraction(tokenSource string, tokenHash string) {
	dl.Logf("=== TOKEN EXTRACTION ===\n  source: %s\n  hash: %s", tokenSource, maskValue(tokenHash))
}

// LogPolicyLookup logs policy lookup
func (dl *DetailedLogger) LogPolicyLookup(tokenHash string, found bool) {
	status := "NOT FOUND"
	if found {
		status = "FOUND"
	}
	dl.Logf("=== POLICY LOOKUP ===\n  hash: %s\n  status: %s", maskValue(tokenHash), status)
}

// LogPolicyDetails logs policy details
func (dl *DetailedLogger) LogPolicyDetails(baseKeyEnv, providerName, upstreamURL string) {
	dl.Logf("=== POLICY DETAILS ===\n  base_key_env: %s\n  provider: %s\n  upstream: %s",
		baseKeyEnv, providerName, upstreamURL)
}

// LogEnvVarLoad logs environment variable loading
func (dl *DetailedLogger) LogEnvVarLoad(envKey string, found bool, value string) {
	status := "NOT SET"
	valDisplay := ""
	if found {
		status = "FOUND"
		valDisplay = fmt.Sprintf(" value=%s", maskValue(value))
	}
	dl.Logf("=== ENV VAR LOAD ===\n  key: %s\n  status: %s%s", envKey, status, valDisplay)
}

// LogAuthHeaderConstruction logs auth header construction
func (dl *DetailedLogger) LogAuthHeaderConstruction(scheme string, headerName string, keySet bool) {
	keyStatus := "not set"
	if keySet {
		keyStatus = "set"
	}
	dl.Logf("=== AUTH HEADER ===\n  scheme: %s\n  header: %s\n  key_status: %s", scheme, headerName, keyStatus)
}

// LogUpstreamCall logs upstream API call
func (dl *DetailedLogger) LogUpstreamCall(method, url, authScheme string) {
	dl.Logf("=== UPSTREAM CALL ===\n  method: %s\n  url: %s\n  auth_scheme: %s", method, url, authScheme)
}

// LogUpstreamResponse logs upstream response
func (dl *DetailedLogger) LogUpstreamResponse(statusCode int, statusText string, duration time.Duration) {
	dl.Logf("=== UPSTREAM RESPONSE ===\n  status: %d %s\n  duration: %dms", statusCode, statusText, duration.Milliseconds())
}

// LogError logs an error
func (dl *DetailedLogger) LogError(context string, err error) {
	dl.Logf("=== ERROR ===\n  context: %s\n  error: %v", context, err)
}

// LogRequestComplete logs a completed request
func (dl *DetailedLogger) LogRequestComplete(method, path, model, provider string, inputTokens, outputTokens, statusCode int) {
	dl.Logf("=== REQUEST COMPLETE ===\n  method: %s\n  path: %s\n  model: %s\n  provider: %s\n  input_tokens: %d\n  output_tokens: %d\n  status: %d",
		method, path, model, provider, inputTokens, outputTokens, statusCode)
}

// LogProxyStartup logs proxy startup
func (dl *DetailedLogger) LogProxyStartup(httpPort, httpsPort int, tlsEnabled bool) {
	dl.Logf("=== PROXY STARTUP ===\n  timestamp: %s\n  http_port: %d\n  https_port: %d\n  tls_enabled: %v",
		time.Now().Format(time.RFC3339), httpPort, httpsPort, tlsEnabled)
}

// LogProxyShutdown logs proxy shutdown
func (dl *DetailedLogger) LogProxyShutdown() {
	dl.Logf("=== PROXY SHUTDOWN ===\n  timestamp: %s", time.Now().Format(time.RFC3339))
}

// Close closes the logger
func (dl *DetailedLogger) Close() error {
	return nil
}

var globalDetailedLogger *DetailedLogger

func init() {
	globalDetailedLogger = &DetailedLogger{}
}

// GetDetailedLogger returns the global detailed logger
func GetDetailedLogger() *DetailedLogger {
	return globalDetailedLogger
}

// InitDetailedLogger reinitializes the global detailed logger with a configured directory
func InitDetailedLogger(dir string) {
	globalDetailedLogger = &DetailedLogger{}
}
