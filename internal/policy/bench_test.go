package policy

import (
	"testing"
)

func BenchmarkParse_Simple(b *testing.B) {
	data := `{"base_key_env":"OPENAI_API_KEY","max_tokens":100000}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatalf("parse failed: %v", err)
		}
	}
}

func BenchmarkParse_MultiProvider(b *testing.B) {
	data := `{
		"providers": {
			"openai": [{"base_key_env":"OPENAI_API_KEY","model_regex":"^gpt"}],
			"anthropic": [{"base_key_env":"ANTHROPIC_API_KEY","model_regex":"^claude"}],
			"groq": [{"base_key_env":"GROQ_API_KEY","model_regex":"^llama"}]
		},
		"rules": [
			{"type":"keyword","keywords":["drop table","rm -rf"],"action":"fail"},
			{"type":"pii","detect":["ssn","credit_card"],"action":"mask","scope":"input"}
		]
	}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatalf("parse failed: %v", err)
		}
	}
}

func BenchmarkResolveForModel_Direct(b *testing.B) {
	pol, _ := Parse(`{
		"providers": {
			"openai": [{"base_key_env":"OPENAI_API_KEY","model":"gpt-4o"}],
			"anthropic": [{"base_key_env":"ANTHROPIC_API_KEY","model_regex":"^claude"}]
		}
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pol.ResolveForModel("gpt-4o")
	}
}

func BenchmarkResolveForModel_Regex(b *testing.B) {
	pol, _ := Parse(`{
		"providers": {
			"openai": [{"base_key_env":"OPENAI_API_KEY","model_regex":"^gpt"}],
			"anthropic": [{"base_key_env":"ANTHROPIC_API_KEY","model_regex":"^claude"}]
		}
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pol.ResolveForModel("claude-3-opus")
	}
}

func BenchmarkResolveForModel_NoMatch(b *testing.B) {
	pol, _ := Parse(`{
		"base_key_env": "DEFAULT_KEY",
		"providers": {
			"openai": [{"base_key_env":"OPENAI_API_KEY","model_regex":"^gpt"}],
			"anthropic": [{"base_key_env":"ANTHROPIC_API_KEY","model_regex":"^claude"}]
		}
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pol.ResolveForModel("unknown-model-xyz")
	}
}

func BenchmarkCheckModel_Allowed(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","model_regex":"^gpt"}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := resolved.CheckModel("gpt-4o"); err != nil {
			b.Fatalf("check model failed: %v", err)
		}
	}
}

func BenchmarkCheckModel_Blocked(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","model_regex":"^gpt"}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := resolved.CheckModel("claude-3-opus"); err == nil {
			b.Fatal("expected model to be blocked")
		}
	}
}

func BenchmarkCheckRules_NoRules(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY"}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolved.CheckRules("some user input text", "input"); err != nil {
			b.Fatalf("check rules failed: %v", err)
		}
	}
}

func BenchmarkCheckRules_KeywordMatch(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","rules":[{"type":"keyword","keywords":["password","secret","token","credentials"],"action":"warn"}]}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolved.CheckRules("please help me reset my password", "input"); err != nil {
			b.Fatalf("check rules failed: %v", err)
		}
	}
}

func BenchmarkCheckRules_KeywordNoMatch(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","rules":[{"type":"keyword","keywords":["password","secret","token","credentials"],"action":"warn"}]}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolved.CheckRules("how do I sort a list in python?", "input"); err != nil {
			b.Fatalf("check rules failed: %v", err)
		}
	}
}

func BenchmarkCheckRules_PII(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","rules":[{"type":"pii","detect":["ssn","credit_card","email","phone"],"action":"mask","scope":"input"}]}`)
	resolved := pol.ResolveForModel("gpt-4o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolved.CheckRules("my email is user@example.com and SSN is 123-45-6789", "input"); err != nil {
			b.Fatalf("check rules failed: %v", err)
		}
	}
}

func BenchmarkMaskContent(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","rules":[{"type":"pii","detect":["ssn","credit_card","email"],"action":"mask","scope":"input"}]}`)
	resolved := pol.ResolveForModel("gpt-4o")
	content := "Contact me at user@example.com, SSN 123-45-6789, card 4111111111111111"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved.MaskContent(content, "input")
	}
}

func BenchmarkInjectPrompts_None(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY"}`)
	resolved := pol.ResolveForModel("gpt-4o")
	msgs := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved.InjectPrompts(msgs)
	}
}

func BenchmarkInjectPrompts_SystemPrompt(b *testing.B) {
	pol, _ := Parse(`{"base_key_env":"KEY","prompts":[{"role":"system","content":"You are a helpful assistant."}]}`)
	resolved := pol.ResolveForModel("gpt-4o")
	msgs := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved.InjectPrompts(msgs)
	}
}

func BenchmarkPolicyJSON(b *testing.B) {
	pol, _ := Parse(`{
		"base_key_env":"KEY",
		"max_tokens":100000,
		"model_regex":"^gpt",
		"rules":[{"type":"keyword","keywords":["secret"],"action":"warn"}],
		"prompts":[{"role":"system","content":"Be helpful"}]
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pol.JSON()
	}
}
