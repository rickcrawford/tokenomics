package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestParseExpires_Empty(t *testing.T) {
	result, err := parseExpires("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestParseExpires_Clear(t *testing.T) {
	result, err := parseExpires("clear")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "clear" {
		t.Errorf("expected 'clear', got %q", result)
	}
}

func TestParseExpires_RFC3339(t *testing.T) {
	ts := "2025-12-31T23:59:59Z"
	result, err := parseExpires(ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ts {
		t.Errorf("expected %q, got %q", ts, result)
	}
}

func TestParseExpires_Days(t *testing.T) {
	before := time.Now().UTC()
	result, err := parseExpires("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Fatalf("invalid RFC3339 result: %v", err)
	}

	expected := before.Add(7 * 24 * time.Hour)
	diff := parsed.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("7d result %v not within 1s of expected %v", parsed, expected)
	}
}

func TestParseExpires_Years(t *testing.T) {
	before := time.Now().UTC()
	result, err := parseExpires("1y")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Fatalf("invalid RFC3339 result: %v", err)
	}

	expected := before.AddDate(1, 0, 0)
	diff := parsed.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("1y result %v not within 1s of expected %v", parsed, expected)
	}
}

func TestParseExpires_GoDuration(t *testing.T) {
	before := time.Now().UTC()
	result, err := parseExpires("24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Fatalf("invalid RFC3339 result: %v", err)
	}

	expected := before.Add(24 * time.Hour)
	diff := parsed.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("24h result %v not within 1s of expected %v", parsed, expected)
	}
}

func TestParseExpires_InvalidInput(t *testing.T) {
	_, err := parseExpires("garbage")
	if err == nil {
		t.Error("expected error for invalid input")
	}
	if !strings.Contains(err.Error(), "invalid expiration") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseExpires_NegativeDuration(t *testing.T) {
	_, err := parseExpires("-1h")
	if err == nil {
		t.Error("expected error for negative duration")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHashToken(t *testing.T) {
	key := []byte("test-key")
	hash1 := hashToken("tkn_abc123", key)
	hash2 := hashToken("tkn_abc123", key)

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}

	hash3 := hashToken("tkn_different", key)
	if hash1 == hash3 {
		t.Error("different input should produce different hash")
	}

	// Different key should produce different hash
	hash4 := hashToken("tkn_abc123", []byte("other-key"))
	if hash1 == hash4 {
		t.Error("different key should produce different hash")
	}
}

func TestGetHashKey_DefaultFallback(t *testing.T) {
	key := getHashKey("NONEXISTENT_ENV_VAR_12345")
	if string(key) != "tokenomics-default-key-change-me" {
		t.Errorf("expected default key, got %q", string(key))
	}
}

func TestGetHashKey_FromEnv(t *testing.T) {
	t.Setenv("TEST_HASH_KEY_12345", "my-secret-key")
	key := getHashKey("TEST_HASH_KEY_12345")
	if string(key) != "my-secret-key" {
		t.Errorf("expected 'my-secret-key', got %q", string(key))
	}
}
