package policy

import (
	"fmt"
)

// CheckModel verifies that the requested model is allowed by the resolved policy.
func (rp *ResolvedPolicy) CheckModel(model string) error {
	if rp.Model != "" && model != rp.Model {
		return fmt.Errorf("model %q not allowed; policy requires %q", model, rp.Model)
	}
	if rp.compiledModelRegex != nil && !rp.compiledModelRegex.MatchString(model) {
		return fmt.Errorf("model %q does not match policy regex %q", model, rp.ModelRegex)
	}
	return nil
}

// CheckRules checks if any user message content matches a blocked pattern.
func (rp *ResolvedPolicy) CheckRules(content string) error {
	for i, r := range rp.compiledRules {
		if r.MatchString(content) {
			rule := ""
			if i < len(rp.Rules) {
				rule = rp.Rules[i]
			}
			return fmt.Errorf("request blocked by rule %d: %q", i, rule)
		}
	}
	return nil
}

// InjectPrompts prepends the policy's system prompts to a messages list.
func (rp *ResolvedPolicy) InjectPrompts(messages []map[string]interface{}) []map[string]interface{} {
	if len(rp.Prompts) == 0 {
		return messages
	}
	injected := make([]map[string]interface{}, 0, len(rp.Prompts)+len(messages))
	for _, pm := range rp.Prompts {
		injected = append(injected, map[string]interface{}{
			"role":    pm.Role,
			"content": pm.Content,
		})
	}
	injected = append(injected, messages...)
	return injected
}

// CheckModel verifies the model on the global policy directly (backward compat).
func (p *Policy) CheckModel(model string) error {
	return p.ResolveProvider("").CheckModel(model)
}

// CheckRules checks rules on the global policy directly (backward compat).
func (p *Policy) CheckRules(content string) error {
	return p.ResolveProvider("").CheckRules(content)
}

// InjectPrompts injects prompts from the global policy directly (backward compat).
func (p *Policy) InjectPrompts(messages []map[string]interface{}) []map[string]interface{} {
	return p.ResolveProvider("").InjectPrompts(messages)
}
