package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
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

// ProviderPolicy holds a single model-scoped policy within a provider.
// Each provider can have multiple policies, each targeting different models.
type ProviderPolicy struct {
	BaseKeyEnv  string    `json:"base_key_env"`
	UpstreamURL string    `json:"upstream_url,omitempty"`
	MaxTokens   int64     `json:"max_tokens,omitempty"`
	Model       string    `json:"model,omitempty"`
	ModelRegex  string    `json:"model_regex,omitempty"`
	Prompts     []Message `json:"prompts,omitempty"`
	Rules       []string  `json:"rules,omitempty"`
	Timeout     int       `json:"timeout,omitempty"` // Per-request timeout in seconds

	// Compiled regexes (not serialized)
	compiledModelRegex *regexp.Regexp
	compiledRules      []*regexp.Regexp
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
	Rules       []string  `json:"rules,omitempty"`

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
	compiledRules      []*regexp.Regexp
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

	p.compiledRules = make([]*regexp.Regexp, 0, len(p.Rules))
	for _, rule := range p.Rules {
		r, err := regexp.Compile(rule)
		if err != nil {
			return fmt.Errorf("invalid rule regex %q: %w", rule, err)
		}
		p.compiledRules = append(p.compiledRules, r)
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

	pp.compiledRules = make([]*regexp.Regexp, 0, len(pp.Rules))
	for _, rule := range pp.Rules {
		r, err := regexp.Compile(rule)
		if err != nil {
			return fmt.Errorf("invalid rule regex %q: %w", rule, err)
		}
		pp.compiledRules = append(pp.compiledRules, r)
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
				return mergeProvider(resolved, pp, p.compiledRules)
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
	return mergeProvider(resolved, policies[0], p.compiledRules)
}

// baseResolved creates a ResolvedPolicy from the global fields.
func (p *Policy) baseResolved() *ResolvedPolicy {
	return &ResolvedPolicy{
		BaseKeyEnv:         p.BaseKeyEnv,
		UpstreamURL:        p.UpstreamURL,
		MaxTokens:          p.MaxTokens,
		Model:              p.Model,
		ModelRegex:         p.ModelRegex,
		Prompts:            p.Prompts,
		Rules:              p.Rules,
		Memory:             p.Memory,
		RateLimit:          p.RateLimit,
		Retry:              p.Retry,
		Timeout:            p.Timeout,
		Metadata:           p.Metadata,
		compiledModelRegex: p.compiledModelRegex,
		compiledRules:      p.compiledRules,
	}
}

func mergeProvider(resolved *ResolvedPolicy, pp *ProviderPolicy, globalRules []*regexp.Regexp) *ResolvedPolicy {
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

	// Provider rules added after global rules
	if len(pp.Rules) > 0 {
		resolved.Rules = append(resolved.Rules, pp.Rules...)
		allRules := make([]*regexp.Regexp, len(globalRules))
		copy(allRules, globalRules)
		allRules = append(allRules, pp.compiledRules...)
		resolved.compiledRules = allRules
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

func (p *Policy) CompiledRules() []*regexp.Regexp {
	return p.compiledRules
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
	Rules        []string
	Memory       MemoryConfig
	RateLimit    *RateLimitConfig
	Retry        *RetryConfig
	Timeout      int
	Metadata     map[string]string

	compiledModelRegex *regexp.Regexp
	compiledRules      []*regexp.Regexp
}

func (rp *ResolvedPolicy) CompiledModelRegex() *regexp.Regexp {
	return rp.compiledModelRegex
}

func (rp *ResolvedPolicy) CompiledRules() []*regexp.Regexp {
	return rp.compiledRules
}
