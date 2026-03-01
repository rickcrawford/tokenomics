package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func TestCloneHeadersForEvent_StripsAuthAndKeys(t *testing.T) {
	h := make(http.Header)
	h.Set("Authorization", "Bearer secret")
	h.Set("x-api-key", "abc123")
	h.Set("X-Api-Key", "abc123")
	h.Set("Content-Type", "application/json")

	got := cloneHeadersForEvent(h)
	if _, ok := got["Authorization"]; ok {
		t.Fatal("expected Authorization header to be removed")
	}
	if _, ok := got["x-api-key"]; ok {
		t.Fatal("expected x-api-key header to be removed")
	}
	if _, ok := got["X-Api-Key"]; ok {
		t.Fatal("expected X-Api-Key header to be removed")
	}
	if got["Content-Type"][0] != "application/json" {
		t.Fatalf("expected Content-Type to be preserved, got %v", got["Content-Type"])
	}
}

func TestBodyForEvent_BinaryAndUTF8(t *testing.T) {
	text, bytes := bodyForEvent("application/octet-stream", []byte{0x00, 0x01, 0xff}, 1024)
	if text != "[binary]" {
		t.Fatalf("expected [binary], got %q", text)
	}
	if bytes != 3 {
		t.Fatalf("expected 3 bytes, got %d", bytes)
	}

	text, bytes = bodyForEvent("application/json", []byte("hello"), 1024)
	if text != "hello" || bytes != 5 {
		t.Fatalf("unexpected text body result: text=%q bytes=%d", text, bytes)
	}
}

func TestExtractSSEDataPayloads_Order(t *testing.T) {
	raw := strings.Join([]string{
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}",
		"",
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}",
		"",
		"data: [DONE]",
	}, "\n")

	got := extractSSEDataPayloads([]byte(raw), 10)
	if len(got) != 3 {
		t.Fatalf("expected 3 SSE payloads, got %d", len(got))
	}
	if got[0] == got[1] {
		t.Fatal("expected ordered unique payloads for first two chunks")
	}
	if got[2] != "[DONE]" {
		t.Fatalf("expected [DONE] as last payload, got %q", got[2])
	}
}
