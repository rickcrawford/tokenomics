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
