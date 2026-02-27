package policy

import (
	"fmt"
	"regexp"
	"strings"
)

// PIIType represents a category of personally identifiable information.
type PIIType string

const (
	PIITypeSSN              PIIType = "ssn"
	PIITypeCreditCard       PIIType = "credit_card"
	PIITypeEmail            PIIType = "email"
	PIITypePhone            PIIType = "phone"
	PIITypeIPAddress        PIIType = "ip_address"
	PIITypeAWSKey           PIIType = "aws_key"
	PIITypeAPIKey           PIIType = "api_key"
	PIITypeJWT              PIIType = "jwt"
	PIITypePrivateKey       PIIType = "private_key"
	PIITypeConnectionString PIIType = "connection_string"
	PIITypeGitHubToken      PIIType = "github_token"
)

// piiDetector holds a compiled pattern for a PII type.
type piiDetector struct {
	piiType PIIType
	pattern *regexp.Regexp
	label   string // Human-readable label for logs
}

// jailbreakPattern represents a regex pattern for detecting jailbreak attempts
type jailbreakPattern struct {
	name    string
	pattern string
}

// jailbreakPatterns contains regex patterns for common jailbreak/prompt injection attempts
var jailbreakPatterns = []jailbreakPattern{
	// Instruction override attempts
	{"ignore_previous", `(?i)ignore\s+(your\s+)?previous\s+(instructions|rules|guidelines)`},
	{"forget_previous", `(?i)forget\s+(all\s+)?(previous|earlier|prior)\s+(instructions|rules|constraints)`},
	{"disregard_instructions", `(?i)(disregard|abandon|drop|remove)\s+(all\s+)?(instructions|rules|constraints|guidelines)`},

	// Developer/test mode claims
	{"developer_mode", `(?i)(developer\s+mode|test\s+mode|debug\s+mode|you\s+are\s+in\s+\w+\s+mode)`},
	{"bypass_safety", `(?i)(bypass|disable|remove|override|circumvent)\s+(all\s+)?(safety|security|ethical|content\s+)?filters?`},

	// Role-playing/actor requests (combined with harmful modifiers)
	{"harmful_roleplay", `(?i)(act\s+as|roleplay|pretend|imagine|assume|you\s+are)\s+(\w+\s+)*(unrestricted|unconstrained|unfiltered|jailbroken|hacker|evil|malicious|unethical)`},

	// Hypothetical/research framing with harmful intent
	{"hypothetical_harm", `(?i)(hypothetically|for\s+research|for\s+academic|as\s+a\s+test|in\s+a\s+scenario).*(exploit|harmful|illegal|unethical|violence|hack)`},

	// Jailbreak tool names (common known jailbreaks)
	{"known_jailbreaks", `(?i)\b(DAN|UCAR|Coach|AIM|STAN|TalkGPT|BadGPT|BadAI)\s*:`},
}

// Pre-compiled jailbreak patterns for performance
var compiledJailbreakPatterns []*regexp.Regexp

// init pre-compiles jailbreak patterns on startup
func initJailbreakPatterns() {
	for _, p := range jailbreakPatterns {
		if re, err := regexp.Compile(p.pattern); err == nil {
			compiledJailbreakPatterns = append(compiledJailbreakPatterns, re)
		}
	}
}

// detectJailbreakFast checks if content contains jailbreak attempt patterns using pre-compiled regexes
func detectJailbreakFast(content string) bool {
	if len(compiledJailbreakPatterns) == 0 {
		return false
	}
	for _, re := range compiledJailbreakPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}

// builtinPII maps PII type names to their detection patterns.
var builtinPII = map[PIIType]struct {
	pattern string
	label   string
}{
	PIITypeSSN: {
		pattern: `\b\d{3}-\d{2}-\d{4}\b`,
		label:   "Social Security Number",
	},
	PIITypeCreditCard: {
		// Visa, Mastercard, Amex, Discover, Diners, JCB patterns
		pattern: `\b(?:4\d{3}|5[1-5]\d{2}|3[47]\d{2}|6(?:011|5\d{2}))\d{4,}\d{4,}\b`,
		label:   "Credit Card Number",
	},
	PIITypeEmail: {
		pattern: `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`,
		label:   "Email Address",
	},
	PIITypePhone: {
		// US phone formats: (555) 123-4567, 555-123-4567, +1-555-123-4567, 5551234567
		pattern: `\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`,
		label:   "Phone Number",
	},
	PIITypeIPAddress: {
		pattern: `\b(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`,
		label:   "IP Address",
	},
	PIITypeAWSKey: {
		pattern: `\b(?:AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}\b`,
		label:   "AWS Access Key",
	},
	PIITypeAPIKey: {
		// Common API key patterns: sk-..., key-..., api_..., token_...
		pattern: `\b(?:sk|key|api|token|secret)[-_][A-Za-z0-9]{20,}\b`,
		label:   "API Key",
	},
	PIITypeJWT: {
		pattern: `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`,
		label:   "JSON Web Token",
	},
	PIITypePrivateKey: {
		pattern: `-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----`,
		label:   "Private Key",
	},
	PIITypeConnectionString: {
		pattern: `\b(?:mongodb|postgres|mysql|redis)://[^\s]+\b`,
		label:   "Database Connection String",
	},
	PIITypeGitHubToken: {
		pattern: `\b(?:ghp|gho|ghs|ghr)_[A-Za-z0-9]{36,}\b`,
		label:   "GitHub Token",
	},
}

// compilePIIDetectors returns compiled detectors for the given PII types.
func compilePIIDetectors(types []string) ([]*piiDetector, error) {
	var detectors []*piiDetector
	for _, t := range types {
		piiType := PIIType(strings.ToLower(t))
		spec, ok := builtinPII[piiType]
		if !ok {
			return nil, fmt.Errorf("unknown PII type %q; available: ssn, credit_card, email, phone, ip_address, aws_key, api_key, jwt, private_key, connection_string, github_token", t)
		}
		r, err := regexp.Compile(spec.pattern)
		if err != nil {
			return nil, fmt.Errorf("compile PII pattern for %s: %w", t, err)
		}
		detectors = append(detectors, &piiDetector{
			piiType: piiType,
			pattern: r,
			label:   spec.label,
		})
	}
	return detectors, nil
}

// detectPII checks content against PII detectors and returns all matches.
func detectPII(content string, detectors []*piiDetector) []PIIMatch {
	var matches []PIIMatch
	for _, d := range detectors {
		found := d.pattern.FindAllString(content, -1)
		for _, f := range found {
			matches = append(matches, PIIMatch{
				Type:  d.piiType,
				Label: d.label,
				Value: f,
			})
		}
	}
	return matches
}

// maskPII replaces PII matches in content with [REDACTED].
func maskPII(content string, detectors []*piiDetector) string {
	for _, d := range detectors {
		content = d.pattern.ReplaceAllString(content, "[REDACTED]")
	}
	return content
}

// PIIMatch represents a detected PII occurrence.
type PIIMatch struct {
	Type  PIIType `json:"type"`
	Label string  `json:"label"`
	Value string  `json:"value"`
}

// init pre-compiles patterns on startup for performance
func init() {
	initJailbreakPatterns()
}
