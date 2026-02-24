package policy

import (
	"strings"
	"testing"
)

func TestCheckModel(t *testing.T) {
	tests := []struct {
		name       string
		policyJSON string
		model      string
		wantErr    string
	}{
		{
			name:       "no model restriction allows anything",
			policyJSON: `{"base_key_env":"K"}`,
			model:      "gpt-4",
			wantErr:    "",
		},
		{
			name:       "exact model match passes",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "gpt-4",
			wantErr:    "",
		},
		{
			name:       "exact model mismatch fails",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "gpt-3.5-turbo",
			wantErr:    "not allowed",
		},
		{
			name:       "regex match passes",
			policyJSON: `{"base_key_env":"K","model_regex":"^gpt-4"}`,
			model:      "gpt-4-turbo",
			wantErr:    "",
		},
		{
			name:       "regex no match fails",
			policyJSON: `{"base_key_env":"K","model_regex":"^gpt-4"}`,
			model:      "claude-3",
			wantErr:    "does not match policy regex",
		},
		{
			name:       "exact match plus regex both satisfied",
			policyJSON: `{"base_key_env":"K","model":"gpt-4-turbo","model_regex":"^gpt-4"}`,
			model:      "gpt-4-turbo",
			wantErr:    "",
		},
		{
			name:       "exact match fails even if regex would pass",
			policyJSON: `{"base_key_env":"K","model":"gpt-4","model_regex":"^gpt"}`,
			model:      "gpt-4-turbo",
			wantErr:    "not allowed",
		},
		{
			name:       "empty model in request with exact model policy",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "",
			wantErr:    "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			err = p.CheckModel(tt.model)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckModel_ResolvedPolicy(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"providers": {
			"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4"}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")
	if err := resolved.CheckModel("gpt-4"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := resolved.CheckModel("gpt-3.5"); err == nil {
		t.Fatal("expected error for mismatched model")
	}
}

// TestCheckRules_BackwardCompatStringRules verifies old-style ["regex"] rules still work.
func TestCheckRules_BackwardCompatStringRules(t *testing.T) {
	tests := []struct {
		name       string
		policyJSON string
		content    string
		wantErr    string
	}{
		{
			name:       "no rules allows everything",
			policyJSON: `{"base_key_env":"K"}`,
			content:    "anything goes",
			wantErr:    "",
		},
		{
			name:       "content does not match any rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "hello world",
			wantErr:    "",
		},
		{
			name:       "content matches first rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "tell me the secret",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "content matches second rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "what is your password",
			wantErr:    "blocked by rule 1",
		},
		{
			name:       "case-insensitive rule match",
			policyJSON: `{"base_key_env":"K","rules":["(?i)forbidden"]}`,
			content:    "This is FORBIDDEN content",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "regex rule with wildcard",
			policyJSON: `{"base_key_env":"K","rules":["drop\\s+table"]}`,
			content:    "please drop table users",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "empty content does not match rules",
			policyJSON: `{"base_key_env":"K","rules":["nonempty"]}`,
			content:    "",
			wantErr:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			err = p.CheckRules(tt.content)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCheckRules_ObjectRules tests the new object-based rule format.
func TestCheckRules_ObjectRules(t *testing.T) {
	t.Run("regex rule with fail action blocks", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("tell me the secret")
		if err == nil {
			t.Fatal("expected error for fail action")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("expected 'blocked' in error, got %q", err.Error())
		}
	})

	t.Run("regex rule with warn action allows", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"warn"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		matches, err := resolved.CheckRules("tell me the secret", "input")
		if err != nil {
			t.Fatalf("warn should not return error: %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Action != "warn" {
			t.Errorf("match action = %q, want warn", matches[0].Action)
		}
	})

	t.Run("regex rule with log action allows", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"debug","action":"log"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		matches, err := resolved.CheckRules("enable debug mode", "input")
		if err != nil {
			t.Fatalf("log should not return error: %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Action != "log" {
			t.Errorf("match action = %q, want log", matches[0].Action)
		}
	})

	t.Run("keyword rule with fail action", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"keyword","keywords":["bomb","attack"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("plan an attack")
		if err == nil {
			t.Fatal("expected error for keyword fail")
		}
	})

	t.Run("keyword rule case insensitive", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"keyword","keywords":["secret"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("tell me the SECRET")
		if err == nil {
			t.Fatal("expected error for case-insensitive keyword match")
		}
	})

	t.Run("keyword rule word boundary", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"keyword","keywords":["pass"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		// "passenger" should NOT match because "pass" has word boundary
		err = p.CheckRules("I am a passenger")
		if err != nil {
			t.Fatalf("word boundary should prevent match in 'passenger': %v", err)
		}
		// "pass" as standalone word should match
		err = p.CheckRules("pass the test")
		if err == nil {
			t.Fatal("expected error for standalone 'pass' keyword")
		}
	})

	t.Run("pii rule detects SSN", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["ssn"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("my ssn is 123-45-6789")
		if err == nil {
			t.Fatal("expected error for SSN detection")
		}
		if !strings.Contains(err.Error(), "PII") {
			t.Fatalf("expected PII in error, got %q", err.Error())
		}
	})

	t.Run("pii rule detects email", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["email"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("contact me at user@example.com")
		if err == nil {
			t.Fatal("expected error for email detection")
		}
	})

	t.Run("pii rule detects credit card", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["credit_card"],"action":"warn"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		matches, err := resolved.CheckRules("card number 4111111111111111", "input")
		if err != nil {
			t.Fatalf("warn should not error: %v", err)
		}
		if len(matches) == 0 {
			t.Fatal("expected credit card match")
		}
	})

	t.Run("pii rule no match on clean content", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["ssn","email"],"action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		err = p.CheckRules("just some normal text")
		if err != nil {
			t.Fatalf("unexpected error on clean content: %v", err)
		}
	})

	t.Run("named rule includes name in match", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"name":"sql-injection","type":"regex","pattern":"drop\\s+table","action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		matches, err := resolved.CheckRules("drop table users", "input")
		if err == nil {
			t.Fatal("expected fail error")
		}
		if len(matches) == 0 {
			t.Fatal("expected matches")
		}
		if matches[0].Name != "sql-injection" {
			t.Errorf("match name = %q, want sql-injection", matches[0].Name)
		}
		if !strings.Contains(matches[0].Message, "sql-injection") {
			t.Errorf("match message should contain rule name, got %q", matches[0].Message)
		}
		_ = err
	})
}

// TestCheckRules_Scope verifies scope filtering.
func TestCheckRules_Scope(t *testing.T) {
	t.Run("input scope rule does not match output", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"fail","scope":"input"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		_, err = resolved.CheckRules("secret", "output")
		if err != nil {
			t.Fatalf("input-only rule should not match output: %v", err)
		}
	})

	t.Run("output scope rule does not match input", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"fail","scope":"output"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		_, err = resolved.CheckRules("secret", "input")
		if err != nil {
			t.Fatalf("output-only rule should not match input: %v", err)
		}
	})

	t.Run("both scope rule matches input and output", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"fail","scope":"both"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")

		_, err = resolved.CheckRules("secret", "input")
		if err == nil {
			t.Fatal("both-scope rule should match input")
		}

		_, err = resolved.CheckRules("secret", "output")
		if err == nil {
			t.Fatal("both-scope rule should match output")
		}
	})

	t.Run("default scope is input", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"fail"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")

		_, err = resolved.CheckRules("secret", "input")
		if err == nil {
			t.Fatal("default scope should match input")
		}

		_, err = resolved.CheckRules("secret", "output")
		if err != nil {
			t.Fatal("default scope should not match output")
		}
	})
}

// TestCheckRules_MixedActions verifies multiple rules with different actions.
func TestCheckRules_MixedActions(t *testing.T) {
	policyJSON := `{
		"base_key_env": "K",
		"rules": [
			{"type": "regex", "pattern": "harmless", "action": "log"},
			{"type": "regex", "pattern": "suspicious", "action": "warn"},
			{"type": "regex", "pattern": "dangerous", "action": "fail"}
		]
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatal(err)
	}
	resolved := p.ResolveProvider("")

	// Content triggers log and warn rules
	matches, err := resolved.CheckRules("harmless and suspicious content", "input")
	if err != nil {
		t.Fatalf("log+warn should not return error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Action != "log" {
		t.Errorf("first match action = %q, want log", matches[0].Action)
	}
	if matches[1].Action != "warn" {
		t.Errorf("second match action = %q, want warn", matches[1].Action)
	}

	// Content triggers fail — should stop at fail
	matches, err = resolved.CheckRules("dangerous content", "input")
	if err == nil {
		t.Fatal("fail action should return error")
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (fail), got %d", len(matches))
	}
	if matches[0].Action != "fail" {
		t.Errorf("match action = %q, want fail", matches[0].Action)
	}
}

// TestMaskContent verifies content masking for different rule types.
func TestMaskContent(t *testing.T) {
	t.Run("regex mask", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"\\d{3}-\\d{2}-\\d{4}","action":"mask"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		result := resolved.MaskContent("my ssn is 123-45-6789", "input")
		if strings.Contains(result, "123-45-6789") {
			t.Error("SSN should be redacted")
		}
		if !strings.Contains(result, "[REDACTED]") {
			t.Error("expected [REDACTED] in output")
		}
	})

	t.Run("keyword mask", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"keyword","keywords":["password","secret"],"action":"mask"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		result := resolved.MaskContent("my password is secret123", "input")
		if !strings.Contains(result, "[REDACTED]") {
			t.Error("expected [REDACTED] in output")
		}
	})

	t.Run("pii mask SSN", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["ssn"],"action":"mask"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		result := resolved.MaskContent("ssn: 123-45-6789", "input")
		if strings.Contains(result, "123-45-6789") {
			t.Error("SSN should be redacted")
		}
		if !strings.Contains(result, "[REDACTED]") {
			t.Error("expected [REDACTED] in output")
		}
	})

	t.Run("pii mask email", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"pii","detect":["email"],"action":"mask"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		result := resolved.MaskContent("email: user@example.com", "input")
		if strings.Contains(result, "user@example.com") {
			t.Error("email should be redacted")
		}
	})

	t.Run("mask only applies to matching scope", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"mask","scope":"output"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")

		// Input scope should not mask
		result := resolved.MaskContent("my secret", "input")
		if !strings.Contains(result, "secret") {
			t.Error("output-only mask should not apply to input")
		}

		// Output scope should mask
		result = resolved.MaskContent("my secret", "output")
		if strings.Contains(result, "secret") {
			t.Error("output mask should apply to output")
		}
	})

	t.Run("non-mask rules do not redact", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"secret","action":"warn"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		result := resolved.MaskContent("my secret", "input")
		if !strings.Contains(result, "secret") {
			t.Error("warn action should not mask content")
		}
	})
}

// TestHasOutputRules tests the HasOutputRules helper.
func TestHasOutputRules(t *testing.T) {
	t.Run("no rules", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K"}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		if resolved.HasOutputRules() {
			t.Error("expected no output rules")
		}
	})

	t.Run("input only rules", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"x","action":"fail","scope":"input"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		if resolved.HasOutputRules() {
			t.Error("expected no output rules for input-only rule")
		}
	})

	t.Run("output scope rule", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"x","action":"fail","scope":"output"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		if !resolved.HasOutputRules() {
			t.Error("expected output rules")
		}
	})

	t.Run("both scope rule", func(t *testing.T) {
		p, err := Parse(`{"base_key_env":"K","rules":[{"type":"regex","pattern":"x","action":"fail","scope":"both"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolveProvider("")
		if !resolved.HasOutputRules() {
			t.Error("expected output rules for both-scope rule")
		}
	})
}

// TestCheckRules_ResolvedMergedRules tests that global and provider rules are merged.
func TestCheckRules_ResolvedMergedRules(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"rules": ["global_secret"],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"rules": ["provider_secret"]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")

	// Should block global rule
	if _, err := resolved.CheckRules("contains global_secret", "input"); err == nil {
		t.Fatal("expected error for global rule match")
	}

	// Should block provider rule
	if _, err := resolved.CheckRules("contains provider_secret", "input"); err == nil {
		t.Fatal("expected error for provider rule match")
	}

	// Should allow clean content
	if _, err := resolved.CheckRules("clean content", "input"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckRules_MergedObjectRules tests merged rules with the new object format.
func TestCheckRules_MergedObjectRules(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"rules": [{"type":"regex","pattern":"global_bad","action":"fail"}],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"rules": [{"type":"keyword","keywords":["provider_bad"],"action":"warn"}]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatal(err)
	}

	resolved := p.ResolveForModel("gpt-4")

	// Global fail rule blocks
	_, err = resolved.CheckRules("global_bad content", "input")
	if err == nil {
		t.Fatal("expected error for global fail rule")
	}

	// Provider warn rule returns match without error
	matches, err := resolved.CheckRules("something provider_bad here", "input")
	if err != nil {
		t.Fatalf("warn should not error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 warn match, got %d", len(matches))
	}
	if matches[0].Action != "warn" {
		t.Errorf("expected warn action, got %q", matches[0].Action)
	}
}

// TestRuleValidation tests that invalid rules are rejected during parse.
func TestRuleValidation(t *testing.T) {
	tests := []struct {
		name       string
		policyJSON string
		wantErr    string
	}{
		{
			name:       "invalid action",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"regex","pattern":"x","action":"delete"}]}`,
			wantErr:    "invalid rule action",
		},
		{
			name:       "invalid type",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"fuzzy","pattern":"x","action":"fail"}]}`,
			wantErr:    "invalid rule type",
		},
		{
			name:       "invalid scope",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"regex","pattern":"x","action":"fail","scope":"everywhere"}]}`,
			wantErr:    "invalid rule scope",
		},
		{
			name:       "regex missing pattern",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"regex","action":"fail"}]}`,
			wantErr:    "requires a pattern",
		},
		{
			name:       "invalid regex pattern",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"regex","pattern":"[invalid","action":"fail"}]}`,
			wantErr:    "invalid rule regex",
		},
		{
			name:       "keyword missing keywords",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"keyword","action":"fail"}]}`,
			wantErr:    "requires at least one keyword",
		},
		{
			name:       "keyword empty array",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"keyword","keywords":[],"action":"fail"}]}`,
			wantErr:    "requires at least one keyword",
		},
		{
			name:       "pii missing detect",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"pii","action":"fail"}]}`,
			wantErr:    "requires at least one detect type",
		},
		{
			name:       "pii unknown detect type",
			policyJSON: `{"base_key_env":"K","rules":[{"type":"pii","detect":["fingerprint"],"action":"fail"}]}`,
			wantErr:    "unknown PII type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.policyJSON)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

// TestPIIDetection tests all built-in PII types.
func TestPIIDetection(t *testing.T) {
	tests := []struct {
		name    string
		detect  string
		content string
		match   bool
	}{
		{"ssn match", "ssn", "my ssn is 123-45-6789", true},
		{"ssn no match", "ssn", "just some numbers 123456789", false},
		{"email match", "email", "email: user@example.com", true},
		{"email no match", "email", "no email here", false},
		{"phone match", "phone", "call me at (555) 123-4567", true},
		{"phone no match", "phone", "no phone", false},
		{"ip match", "ip_address", "server at 192.168.1.1", true},
		{"ip no match", "ip_address", "not an ip 999.999.999.999", false},
		{"aws key match", "aws_key", "key is AKIAIOSFODNN7EXAMPLE", true},
		{"aws key no match", "aws_key", "not a key XXXIOSFODNN7EXAMPLE", false},
		{"api key match", "api_key", "use sk-1234567890abcdefghijklmn", true},
		{"api key no match", "api_key", "nothing here", false},
		{"jwt match", "jwt", "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", true},
		{"jwt no match", "jwt", "not a jwt", false},
		{"private key match", "private_key", "-----BEGIN RSA PRIVATE KEY-----", true},
		{"private key ec match", "private_key", "-----BEGIN EC PRIVATE KEY-----", true},
		{"private key no match", "private_key", "just text", false},
		{"connection string match", "connection_string", "connect to postgres://user:pass@host/db", true},
		{"connection string no match", "connection_string", "no connection", false},
		{"github token match", "github_token", "use ghp_1234567890abcdefghijklmnopqrstuvwxyz", true},
		{"github token no match", "github_token", "no token here", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policyJSON := `{"base_key_env":"K","rules":[{"type":"pii","detect":["` + tt.detect + `"],"action":"fail"}]}`
			p, err := Parse(policyJSON)
			if err != nil {
				t.Fatal(err)
			}
			err = p.CheckRules(tt.content)
			if tt.match && err == nil {
				t.Error("expected PII match but got none")
			}
			if !tt.match && err != nil {
				t.Errorf("expected no PII match but got: %v", err)
			}
		})
	}
}

func TestInjectPrompts(t *testing.T) {
	tests := []struct {
		name        string
		policyJSON  string
		messages    []map[string]interface{}
		wantLen     int
		wantFirst   string
		wantContent string
	}{
		{
			name:       "no prompts returns original messages",
			policyJSON: `{"base_key_env":"K"}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:   1,
			wantFirst: "user",
		},
		{
			name:       "single prompt injected before messages",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"be helpful"}]}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:     2,
			wantFirst:   "system",
			wantContent: "be helpful",
		},
		{
			name:       "multiple prompts injected in order",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"first"},{"role":"system","content":"second"}]}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:     3,
			wantFirst:   "system",
			wantContent: "first",
		},
		{
			name:       "empty messages with prompts",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"only prompt"}]}`,
			messages:   []map[string]interface{}{},
			wantLen:    1,
			wantFirst:  "system",
		},
		{
			name:       "nil messages with no prompts",
			policyJSON: `{"base_key_env":"K"}`,
			messages:   nil,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			result := p.InjectPrompts(tt.messages)

			if len(result) != tt.wantLen {
				t.Fatalf("len(result) = %d, want %d", len(result), tt.wantLen)
			}

			if tt.wantLen > 0 && tt.wantFirst != "" {
				if role, ok := result[0]["role"].(string); !ok || role != tt.wantFirst {
					t.Errorf("first message role = %v, want %q", result[0]["role"], tt.wantFirst)
				}
			}

			if tt.wantContent != "" && tt.wantLen > 0 {
				if content, ok := result[0]["content"].(string); !ok || content != tt.wantContent {
					t.Errorf("first message content = %v, want %q", result[0]["content"], tt.wantContent)
				}
			}
		})
	}
}

func TestInjectPrompts_DoesNotMutateOriginal(t *testing.T) {
	p, err := Parse(`{"base_key_env":"K","prompts":[{"role":"system","content":"injected"}]}`)
	if err != nil {
		t.Fatalf("failed to parse policy: %v", err)
	}

	original := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}

	result := p.InjectPrompts(original)

	if len(original) != 1 {
		t.Fatalf("original was mutated: len = %d, want 1", len(original))
	}
	if len(result) != 2 {
		t.Fatalf("result len = %d, want 2", len(result))
	}
}

func TestInjectPrompts_ResolvedMergedPrompts(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"prompts": [{"role": "system", "content": "global prompt"}],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"prompts": [{"role": "system", "content": "provider prompt"}]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")
	messages := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}

	result := resolved.InjectPrompts(messages)

	// Provider prompts prepend before global, then user messages
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0]["content"] != "provider prompt" {
		t.Errorf("first = %q, want %q", result[0]["content"], "provider prompt")
	}
	if result[1]["content"] != "global prompt" {
		t.Errorf("second = %q, want %q", result[1]["content"], "global prompt")
	}
	if result[2]["content"] != "hello" {
		t.Errorf("third = %q, want %q", result[2]["content"], "hello")
	}
}
