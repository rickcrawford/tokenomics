package policy

import (
	"fmt"
)

// CheckModel verifies that the requested model is allowed by the policy.
func (p *Policy) CheckModel(model string) error {
	if p.Model != "" && model != p.Model {
		return fmt.Errorf("model %q not allowed; policy requires %q", model, p.Model)
	}
	if p.compiledModelRegex != nil && !p.compiledModelRegex.MatchString(model) {
		return fmt.Errorf("model %q does not match policy regex %q", model, p.ModelRegex)
	}
	return nil
}

// CheckRules checks if any user message content matches a blocked pattern.
func (p *Policy) CheckRules(content string) error {
	for i, r := range p.compiledRules {
		if r.MatchString(content) {
			return fmt.Errorf("request blocked by rule %d: %q", i, p.Rules[i])
		}
	}
	return nil
}

// InjectPrompts prepends the policy's system prompts to a messages list.
func (p *Policy) InjectPrompts(messages []map[string]interface{}) []map[string]interface{} {
	if len(p.Prompts) == 0 {
		return messages
	}
	injected := make([]map[string]interface{}, 0, len(p.Prompts)+len(messages))
	for _, pm := range p.Prompts {
		injected = append(injected, map[string]interface{}{
			"role":    pm.Role,
			"content": pm.Content,
		})
	}
	injected = append(injected, messages...)
	return injected
}
