package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MemoryConfig controls session memory (conversation logging).
type MemoryConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	FilePath string `json:"file_path,omitempty"` // Append markdown to this file per session
	Redis    bool   `json:"redis,omitempty"`     // Push to Redis collection by session
}

// RateLimitConfig controls request and token throughput.
// Supports multiple rules with different windows and strategies.
type RateLimitConfig struct {
	Rules       []RateLimitRule `json:"rules,omitempty"`
	MaxParallel int             `json:"max_parallel,omitempty"` // Max concurrent requests
}

// RateLimitRule defines a single rate limit window.
type RateLimitRule struct {
	Requests int    `json:"requests,omitempty"` // Max requests per window (0 = unlimited)
	Tokens   int    `json:"tokens,omitempty"`   // Max tokens per window (0 = unlimited)
	Window   string `json:"window,omitempty"`   // Duration: "1s", "1m" (default), "1h", "24h"
	Strategy string `json:"strategy,omitempty"` // "sliding" (default) or "fixed"
}

// RetryConfig controls retry and fallback behavior on upstream errors.
type RetryConfig struct {
	MaxRetries int      `json:"max_retries,omitempty"` // Max retry attempts (default 0)
	Fallbacks  []string `json:"fallbacks,omitempty"`   // Fallback model names to try in order
	RetryOn    []int    `json:"retry_on,omitempty"`    // HTTP status codes that trigger retry (default: 429,500,502,503)
}

// Rule defines a content inspection rule with a type, match criteria, and action.
//
// Rule types:
//   - "regex": Match content against a Go regular expression (Pattern field)
//   - "keyword": Match against a list of case-insensitive keywords (Keywords field)
//   - "pii": Detect personally identifiable information (Detect field: ssn, credit_card, email, phone, ip_address, aws_key, api_key)
//
// Actions:
//   - "fail": Block the request with 403 Forbidden (default)
//   - "warn": Allow the request but log a warning
//   - "log": Silently log the match
//   - "mask": Redact matched content before forwarding (replaces with [REDACTED])
//
// Scope:
//   - "input": Check user messages only (default)
//   - "output": Check response content only
//   - "both": Check both input and output
type Rule struct {
	Name     string   `json:"name,omitempty"`     // Human-readable rule name
	Type     string   `json:"type"`               // "regex", "keyword", "pii"
	Pattern  string   `json:"pattern,omitempty"`  // For regex type
	Keywords []string `json:"keywords,omitempty"` // For keyword type
	Detect   []string `json:"detect,omitempty"`   // For pii type
	Action   string   `json:"action"`             // "fail", "warn", "log", "mask"
	Scope    string   `json:"scope,omitempty"`    // "input" (default), "output", "both"

	// Compiled matchers (not serialized)
	compiledPattern  *regexp.Regexp
	compiledKeywords []*regexp.Regexp
	compiledPII      []*piiDetector
}

// compile validates and compiles the rule's match criteria.
func (r *Rule) compile() error {
	// Default action is fail
	if r.Action == "" {
		r.Action = "fail"
	}

	// Validate action
	switch r.Action {
	case "fail", "warn", "log", "mask":
	default:
		return fmt.Errorf("invalid rule action %q; must be fail, warn, log, or mask", r.Action)
	}

	// Validate scope
	if r.Scope == "" {
		r.Scope = "input"
	}
	switch r.Scope {
	case "input", "output", "both":
	default:
		return fmt.Errorf("invalid rule scope %q; must be input, output, or both", r.Scope)
	}

	// Default type is regex
	if r.Type == "" {
		r.Type = "regex"
	}

	switch r.Type {
	case "regex":
		if r.Pattern == "" {
			return fmt.Errorf("regex rule requires a pattern")
		}
		compiled, err := regexp.Compile(r.Pattern)
		if err != nil {
			return fmt.Errorf("invalid rule regex %q: %w", r.Pattern, err)
		}
		r.compiledPattern = compiled

	case "keyword":
		if len(r.Keywords) == 0 {
			return fmt.Errorf("keyword rule requires at least one keyword")
		}
		r.compiledKeywords = make([]*regexp.Regexp, 0, len(r.Keywords))
		for _, kw := range r.Keywords {
			// Build case-insensitive word boundary pattern
			escaped := regexp.QuoteMeta(kw)
			compiled, err := regexp.Compile(`(?i)\b` + escaped + `\b`)
			if err != nil {
				return fmt.Errorf("invalid keyword %q: %w", kw, err)
			}
			r.compiledKeywords = append(r.compiledKeywords, compiled)
		}

	case "pii":
		if len(r.Detect) == 0 {
			return fmt.Errorf("pii rule requires at least one detect type (ssn, credit_card, email, phone, ip_address, aws_key, api_key)")
		}
		detectors, err := compilePIIDetectors(r.Detect)
		if err != nil {
			return err
		}
		r.compiledPII = detectors

	default:
		return fmt.Errorf("invalid rule type %q; must be regex, keyword, or pii", r.Type)
	}

	return nil
}

// RuleMatch represents the result of a rule check.
type RuleMatch struct {
	Rule    *Rule  `json:"-"`
	Name    string `json:"name,omitempty"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// matches checks if the content triggers this rule. Returns a match description or empty string.
func (r *Rule) matches(content string) string {
	switch r.Type {
	case "regex":
		if r.compiledPattern != nil && r.compiledPattern.MatchString(content) {
			name := r.Name
			if name == "" {
				name = r.Pattern
			}
			return fmt.Sprintf("matched regex rule %q", name)
		}

	case "keyword":
		for _, kw := range r.compiledKeywords {
			if kw.MatchString(content) {
				name := r.Name
				if name == "" {
					name = "keyword"
				}
				return fmt.Sprintf("matched keyword rule %q: %s", name, kw.String())
			}
		}

	case "pii":
		piiMatches := detectPII(content, r.compiledPII)
		if len(piiMatches) > 0 {
			name := r.Name
			if name == "" {
				name = "pii"
			}
			types := make([]string, 0, len(piiMatches))
			for _, m := range piiMatches {
				types = append(types, m.Label)
			}
			return fmt.Sprintf("detected PII in rule %q: %s", name, strings.Join(types, ", "))
		}
	}
	return ""
}

// maskContent redacts matched content and returns the modified string.
func (r *Rule) maskContent(content string) string {
	switch r.Type {
	case "regex":
		if r.compiledPattern != nil {
			return r.compiledPattern.ReplaceAllString(content, "[REDACTED]")
		}
	case "keyword":
		for _, kw := range r.compiledKeywords {
			content = kw.ReplaceAllString(content, "[REDACTED]")
		}
	case "pii":
		content = maskPII(content, r.compiledPII)
	}
	return content
}

// RuleSet holds compiled rules and provides checking methods.
// This is a flexible wrapper used internally by the rules engine.
type RuleSet []Rule

// ProviderPolicy holds a single model-scoped policy within a provider.
// Each provider can have multiple policies, each targeting different models.
type ProviderPolicy struct {
	BaseKeyEnv  string    `json:"base_key_env"`
	UpstreamURL string    `json:"upstream_url,omitempty"`
	MaxTokens   int64     `json:"max_tokens,omitempty"`
	Model       string    `json:"model,omitempty"`
	ModelRegex  string    `json:"model_regex,omitempty"`
	Prompts     []Message `json:"prompts,omitempty"`
	Rules       RuleList  `json:"rules,omitempty"`
	Timeout     int       `json:"timeout,omitempty"` // Per-request timeout in seconds

	// Compiled regexes (not serialized)
	compiledModelRegex *regexp.Regexp
}

// Policy is the top-level policy for a wrapper token.
// Global settings apply to all requests. Providers map holds arrays of
// model-scoped policies per provider. The proxy resolves by matching the
// request model against provider policies.
type Policy struct {
	// Global policy fields — applied first to every request
	BaseKeyEnv  string    `json:"base_key_env,omitempty"`
	UpstreamURL string    `json:"upstream_url,omitempty"`
	MaxTokens   int64     `json:"max_tokens,omitempty"`
	Model       string    `json:"model,omitempty"`
	ModelRegex  string    `json:"model_regex,omitempty"`
	Prompts     []Message `json:"prompts,omitempty"`
	Rules       RuleList  `json:"rules,omitempty"`

	// Per-provider policy arrays keyed by provider name.
	// Each provider has an array of policies — one per model or model pattern.
	// Example: {"openai": [{model:"gpt-4o",...}, {model_regex:"^gpt-3.*",...}]}
	Providers map[string][]*ProviderPolicy `json:"providers,omitempty"`

	// Session memory configuration
	Memory MemoryConfig `json:"memory,omitempty"`

	// Rate limiting
	RateLimit *RateLimitConfig `json:"rate_limit,omitempty"`

	// Retry and fallback
	Retry *RetryConfig `json:"retry,omitempty"`

	// Per-request timeout in seconds (0 = use default)
	Timeout int `json:"timeout,omitempty"`

	// Metadata tags for analytics and cost attribution
	Metadata map[string]string `json:"metadata,omitempty"`

	// Compiled regexes for the global policy (not serialized)
	compiledModelRegex *regexp.Regexp
}

// RuleList supports both old string-array format and new object-array format.
// Old format: ["regex1", "regex2"] → converted to [{type:"regex", pattern:"regex1", action:"fail"}, ...]
// New format: [{type:"regex", pattern:"...", action:"fail"}, {type:"pii", detect:["ssn"], action:"mask"}, ...]
type RuleList []Rule

// UnmarshalJSON handles backward compatibility: accepts both ["string"] and [{...}] formats.
func (rl *RuleList) UnmarshalJSON(data []byte) error {
	// Try new format first (array of objects)
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err == nil {
		*rl = rules
		return nil
	}

	// Fall back to old format (array of strings → regex rules with fail action)
	var strings []string
	if err := json.Unmarshal(data, &strings); err != nil {
		return fmt.Errorf("rules must be an array of rule objects or regex strings: %w", err)
	}

	*rl = make(RuleList, 0, len(strings))
	for _, s := range strings {
		*rl = append(*rl, Rule{
			Type:    "regex",
			Pattern: s,
			Action:  "fail",
			Scope:   "input",
		})
	}
	return nil
}

func Parse(data string) (*Policy, error) {
	var p Policy
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return nil, fmt.Errorf("invalid policy JSON: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Policy) Validate() error {
	// Must have at least one provider or a global base_key_env
	if p.BaseKeyEnv == "" && len(p.Providers) == 0 {
		return fmt.Errorf("base_key_env is required (or at least one provider)")
	}

	// Compile global regexes
	if p.ModelRegex != "" {
		r, err := regexp.Compile(p.ModelRegex)
		if err != nil {
			return fmt.Errorf("invalid model_regex %q: %w", p.ModelRegex, err)
		}
		p.compiledModelRegex = r
	}

	// Compile global rules
	for i := range p.Rules {
		if err := p.Rules[i].compile(); err != nil {
			return fmt.Errorf("global rule %d: %w", i, err)
		}
	}

	// Validate and compile each provider policy
	for name, policies := range p.Providers {
		for i, pp := range policies {
			if pp.BaseKeyEnv == "" {
				return fmt.Errorf("provider %q policy %d: base_key_env is required", name, i)
			}
			if err := pp.compile(); err != nil {
				return fmt.Errorf("provider %q policy %d: %w", name, i, err)
			}
		}
	}

	return nil
}

func (pp *ProviderPolicy) compile() error {
	if pp.ModelRegex != "" {
		r, err := regexp.Compile(pp.ModelRegex)
		if err != nil {
			return fmt.Errorf("invalid model_regex %q: %w", pp.ModelRegex, err)
		}
		pp.compiledModelRegex = r
	}

	for i := range pp.Rules {
		if err := pp.Rules[i].compile(); err != nil {
			return fmt.Errorf("rule %d: %w", i, err)
		}
	}

	return nil
}

// matchesModel returns true if the provider policy allows the given model.
func (pp *ProviderPolicy) matchesModel(model string) bool {
	if pp.Model != "" && pp.Model == model {
		return true
	}
	if pp.compiledModelRegex != nil && pp.compiledModelRegex.MatchString(model) {
		return true
	}
	// If no model constraint, matches everything
	if pp.Model == "" && pp.ModelRegex == "" {
		return true
	}
	return false
}

// ResolveForModel finds the best matching provider policy for the given model,
// applies global policy first, then merges the matching provider policy on top.
// It searches all providers for a model match.
func (p *Policy) ResolveForModel(model string) *ResolvedPolicy {
	// Start with global
	resolved := p.baseResolved()

	// Search providers for a matching policy
	for name, policies := range p.Providers {
		for _, pp := range policies {
			if pp.matchesModel(model) {
				resolved.ProviderName = name
				return mergeProvider(resolved, pp)
			}
		}
	}

	return resolved
}

// ResolveProvider returns the effective policy for a named provider.
// If model is empty, returns the first policy for that provider (or global if not found).
func (p *Policy) ResolveProvider(providerName string) *ResolvedPolicy {
	resolved := p.baseResolved()

	if providerName == "" {
		return resolved
	}

	policies, ok := p.Providers[providerName]
	if !ok || len(policies) == 0 {
		return resolved
	}

	resolved.ProviderName = providerName
	return mergeProvider(resolved, policies[0])
}

// baseResolved creates a ResolvedPolicy from the global fields.
func (p *Policy) baseResolved() *ResolvedPolicy {
	// Copy rules
	rules := make([]Rule, len(p.Rules))
	copy(rules, p.Rules)

	return &ResolvedPolicy{
		BaseKeyEnv:         p.BaseKeyEnv,
		UpstreamURL:        p.UpstreamURL,
		MaxTokens:          p.MaxTokens,
		Model:              p.Model,
		ModelRegex:         p.ModelRegex,
		Prompts:            p.Prompts,
		Rules:              rules,
		Memory:             p.Memory,
		RateLimit:          p.RateLimit,
		Retry:              p.Retry,
		Timeout:            p.Timeout,
		Metadata:           p.Metadata,
		compiledModelRegex: p.compiledModelRegex,
	}
}

func mergeProvider(resolved *ResolvedPolicy, pp *ProviderPolicy) *ResolvedPolicy {
	if pp.BaseKeyEnv != "" {
		resolved.BaseKeyEnv = pp.BaseKeyEnv
	}
	if pp.UpstreamURL != "" {
		resolved.UpstreamURL = pp.UpstreamURL
	}
	if pp.MaxTokens > 0 {
		resolved.MaxTokens = pp.MaxTokens
	}
	if pp.Model != "" {
		resolved.Model = pp.Model
		resolved.compiledModelRegex = nil
	}
	if pp.ModelRegex != "" {
		resolved.ModelRegex = pp.ModelRegex
		resolved.compiledModelRegex = pp.compiledModelRegex
	}

	// Provider timeout overrides global
	if pp.Timeout > 0 {
		resolved.Timeout = pp.Timeout
	}

	// Provider prompts prepend before global prompts
	if len(pp.Prompts) > 0 {
		merged := make([]Message, 0, len(pp.Prompts)+len(resolved.Prompts))
		merged = append(merged, pp.Prompts...)
		merged = append(merged, resolved.Prompts...)
		resolved.Prompts = merged
	}

	// Provider rules appended after global rules
	if len(pp.Rules) > 0 {
		resolved.Rules = append(resolved.Rules, pp.Rules...)
	}

	return resolved
}

// ProviderNames returns all configured provider names.
func (p *Policy) ProviderNames() []string {
	names := make([]string, 0, len(p.Providers))
	for name := range p.Providers {
		names = append(names, name)
	}
	return names
}

func (p *Policy) JSON() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func (p *Policy) CompiledModelRegex() *regexp.Regexp {
	return p.compiledModelRegex
}

// ResolvedPolicy is the effective policy after merging global + provider.
// This is what the proxy handler works with.
type ResolvedPolicy struct {
	BaseKeyEnv   string
	UpstreamURL  string
	MaxTokens    int64
	Model        string
	ModelRegex   string
	ProviderName string // Which provider was matched (key from Providers map)
	Prompts      []Message
	Rules        []Rule
	Memory       MemoryConfig
	RateLimit    *RateLimitConfig
	Retry        *RetryConfig
	Timeout      int
	Metadata     map[string]string

	compiledModelRegex *regexp.Regexp
}

func (rp *ResolvedPolicy) CompiledModelRegex() *regexp.Regexp {
	return rp.compiledModelRegex
}
