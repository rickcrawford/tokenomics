package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputShell(t *testing.T) {
	pairs := []EnvPair{
		{"OPENAI_API_KEY", "tkn_abc123"},
		{"OPENAI_BASE_URL", "https://localhost:8443/v1"},
	}

	f, err := os.CreateTemp(t.TempDir(), "shell-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := OutputShell(pairs, f); err != nil {
		t.Fatalf("OutputShell error: %v", err)
	}

	data, _ := os.ReadFile(f.Name())
	output := string(data)

	if !strings.Contains(output, `export OPENAI_API_KEY="tkn_abc123"`) {
		t.Errorf("expected OPENAI_API_KEY export, got:\n%s", output)
	}
	if !strings.Contains(output, `export OPENAI_BASE_URL="https://localhost:8443/v1"`) {
		t.Errorf("expected OPENAI_BASE_URL export, got:\n%s", output)
	}
}

func TestOutputJSON(t *testing.T) {
	pairs := []EnvPair{
		{"API_KEY", "secret"},
		{"BASE_URL", "http://localhost"},
	}

	f, err := os.CreateTemp(t.TempDir(), "json-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := OutputJSON(pairs, f); err != nil {
		t.Fatalf("OutputJSON error: %v", err)
	}

	data, _ := os.ReadFile(f.Name())
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["API_KEY"] != "secret" {
		t.Errorf("API_KEY = %q, want %q", m["API_KEY"], "secret")
	}
	if m["BASE_URL"] != "http://localhost" {
		t.Errorf("BASE_URL = %q, want %q", m["BASE_URL"], "http://localhost")
	}
}

func TestOutputDotenv_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	pairs := []EnvPair{
		{"KEY1", "val1"},
		{"KEY2", "val2"},
	}

	if err := OutputDotenv(pairs, path); err != nil {
		t.Fatalf("OutputDotenv error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, `KEY1="val1"`) {
		t.Errorf("expected KEY1, got:\n%s", content)
	}
	if !strings.Contains(content, `KEY2="val2"`) {
		t.Errorf("expected KEY2, got:\n%s", content)
	}
	if !strings.HasSuffix(content, "\n") {
		t.Error("expected trailing newline")
	}
}

func TestOutputDotenv_MergesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	// Write initial content
	os.WriteFile(path, []byte("KEY1=\"old\"\nOTHER=\"keep\"\n"), 0o644)

	pairs := []EnvPair{
		{"KEY1", "new"},
		{"KEY3", "added"},
	}

	if err := OutputDotenv(pairs, path); err != nil {
		t.Fatalf("OutputDotenv error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, `KEY1="new"`) {
		t.Errorf("expected updated KEY1, got:\n%s", content)
	}
	if !strings.Contains(content, `OTHER="keep"`) {
		t.Errorf("expected preserved OTHER, got:\n%s", content)
	}
	if !strings.Contains(content, `KEY3="added"`) {
		t.Errorf("expected appended KEY3, got:\n%s", content)
	}
}

func TestOutputDotenv_DefaultPath(t *testing.T) {
	// Change to temp dir so default ".env" doesn't pollute the repo
	orig, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	defer os.Chdir(orig)

	pairs := []EnvPair{{"TEST_KEY", "test_val"}}
	if err := OutputDotenv(pairs, ""); err != nil {
		t.Fatalf("OutputDotenv error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(data), `TEST_KEY="test_val"`) {
		t.Errorf("expected TEST_KEY in default .env, got:\n%s", string(data))
	}
}
