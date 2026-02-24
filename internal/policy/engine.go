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

// CheckRules checks content against all rules with the given scope.
// Returns a list of matches. The caller decides how to handle each action.
// For backward compatibility, also returns a blocking error if any "fail" rule matches.
func (rp *ResolvedPolicy) CheckRules(content string, scope string) ([]RuleMatch, error) {
	var matches []RuleMatch

	for i := range rp.Rules {
		r := &rp.Rules[i]

		// Check scope
		if !ruleMatchesScope(r, scope) {
			continue
		}

		msg := r.matches(content)
		if msg == "" {
			continue
		}

		match := RuleMatch{
			Rule:    r,
			Name:    r.Name,
			Action:  r.Action,
			Message: msg,
		}
		matches = append(matches, match)

		// Fail action blocks immediately
		if r.Action == "fail" {
			return matches, fmt.Errorf("request blocked by rule %d: %s", i, msg)
		}
	}

	return matches, nil
}

// MaskContent applies all "mask" rules to the content for the given scope
// and returns the modified content.
func (rp *ResolvedPolicy) MaskContent(content string, scope string) string {
	for i := range rp.Rules {
		r := &rp.Rules[i]
		if r.Action != "mask" {
			continue
		}
		if !ruleMatchesScope(r, scope) {
			continue
		}
		content = r.maskContent(content)
	}
	return content
}

// ruleMatchesScope returns true if the rule applies to the given scope direction.
func ruleMatchesScope(r *Rule, scope string) bool {
	ruleScope := r.Scope
	if ruleScope == "" {
		ruleScope = "input"
	}
	if ruleScope == "both" {
		return true
	}
	return ruleScope == scope
}

// HasOutputRules returns true if any rules apply to the "output" scope.
func (rp *ResolvedPolicy) HasOutputRules() bool {
	for i := range rp.Rules {
		if rp.Rules[i].Scope == "output" || rp.Rules[i].Scope == "both" {
			return true
		}
	}
	return false
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
	_, err := p.ResolveProvider("").CheckRules(content, "input")
	return err
}

// InjectPrompts injects prompts from the global policy directly (backward compat).
func (p *Policy) InjectPrompts(messages []map[string]interface{}) []map[string]interface{} {
	return p.ResolveProvider("").InjectPrompts(messages)
}
