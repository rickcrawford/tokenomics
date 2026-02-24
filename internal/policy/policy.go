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

type Policy struct {
	BaseKeyEnv  string    `json:"base_key_env"`
	UpstreamURL string    `json:"upstream_url,omitempty"`
	MaxTokens   int64     `json:"max_tokens,omitempty"`
	Model       string    `json:"model,omitempty"`
	ModelRegex  string    `json:"model_regex,omitempty"`
	Prompts     []Message `json:"prompts,omitempty"`
	Rules       []string  `json:"rules,omitempty"`

	// Compiled regexes (not serialized)
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
	if p.BaseKeyEnv == "" {
		return fmt.Errorf("base_key_env is required")
	}

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

	return nil
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
