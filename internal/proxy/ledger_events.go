package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/rickcrawford/tokenomics/internal/ledger"
)

func (h *Handler) recordCommunicationEvent(ev ledger.CommunicationEvent) {
	if h.ledger == nil || !h.ledger.EventsEnabled() {
		return
	}
	if err := h.ledger.RecordCommunicationEvent(ev); err != nil {
		debugLog("failed to record communication event (%s): %v", ev.Type, err)
	}
}

func cloneHeadersForEvent(hdr http.Header) map[string][]string {
	if hdr == nil {
		return nil
	}
	safe := safeHeaders(hdr)
	out := make(map[string][]string, len(safe))
	for k, vals := range safe {
		copied := make([]string, len(vals))
		copy(copied, vals)
		out[k] = copied
	}
	return out
}

func bodyForEvent(contentType string, body []byte, maxBody int) (string, int) {
	if len(body) == 0 {
		return "", 0
	}
	bodyBytes := len(body)
	if maxBody > 0 && bodyBytes > maxBody {
		body = body[:maxBody]
	}
	if !utf8.Valid(body) || isBinaryContentType(contentType) {
		return "[binary]", bodyBytes
	}
	return string(body), bodyBytes
}

func extractSSEDataPayloads(raw []byte, maxEvents int) []string {
	if len(raw) == 0 {
		return nil
	}
	lines := strings.Split(string(raw), "\n")
	events := make([]string, 0, 16)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		events = append(events, payload)
		if maxEvents > 0 && len(events) >= maxEvents {
			break
		}
	}
	return events
}

// formatSSEForMemory renders parsed SSE payloads into a readable memory body.
// It avoids raw "data:" lines while preserving the event sequence.
func formatSSEForMemory(payloads []string, assistantText string, truncated bool) string {
	var b strings.Builder
	b.WriteString("[streaming sse]\n")
	if len(payloads) == 0 {
		b.WriteString("\nSSE events:\n(none captured)\n")
	} else {
		b.WriteString("\nSSE events:\n")
		for i, p := range payloads {
			fmt.Fprintf(&b, "%d. %s\n", i+1, p)
		}
	}
	b.WriteString("\nCaptured assistant text:\n")
	if assistantText == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(assistantText)
		if !strings.HasSuffix(assistantText, "\n") {
			b.WriteString("\n")
		}
	}
	if truncated {
		b.WriteString("\n[assistant content truncated for memory]\n")
	}
	return b.String()
}
