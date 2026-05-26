package celexpr

import (
	"regexp"
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Secret detection — a built-in credential / token scanner exposed as
// two CEL functions over the `body` variable:
//
//	has_secrets(body)   -> bool          (true on the first match)
//	secret_kinds(body)  -> list<string>  (all matched categories, sorted)
//
// Turns file-search-on into a quick triage layer — "find every source
// file in my home that might contain an AWS key" without standing up
// gitleaks / trufflehog. Both functions require the `body` variable,
// so callers must pass --body (CLI) / include_body (MCP).
//
// The pattern catalogue favours ANCHORED prefixes (AKIA…, ghp_…,
// xox[baprs]-…) over fuzzy entropy heuristics — high-confidence, low
// false-positive. Patterns are RE2 (Go regexp; no backreferences) so
// they're safe against catastrophic backtracking on adversarial input.
// Sourced from gitleaks' rule set + the canonical provider key
// formats.
//
// Out of scope (per issue #212): entropy-based detection, live key
// validation, auto-redaction, git-history walking. This is detection
// only; remediation is the user's job.
//
// Issue #212.

// secretPattern pairs a category label with its detection regex.
type secretPattern struct {
	kind string
	re   *regexp.Regexp
}

// secretPatterns is the curated catalogue, compiled once at package
// init. Order doesn't affect correctness (DetectSecrets sorts the
// output) but is grouped by provider for readability.
var secretPatterns = compileSecretPatterns()

func compileSecretPatterns() []secretPattern {
	specs := []struct {
		kind string
		expr string
	}{
		// AWS. Access key IDs have fixed prefixes (AKIA permanent,
		// ASIA temporary, AGPA group, AIDA user, etc.) + 16 base32 chars.
		{"aws-access-key", `\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA)[0-9A-Z]{16}\b`},
		// AWS secret keys are 40-char base64 with no fixed prefix —
		// only match when context-anchored to avoid false positives on
		// any 40-char blob.
		{"aws-secret-key", `(?i)aws_?secret_?(?:access_?)?key["'` + "`" + `]?\s*[:=]\s*["'` + "`" + `]?[A-Za-z0-9/+]{40}`},

		// GitHub. ghp_ classic PAT, gho_ OAuth, ghu_ user-to-server,
		// ghs_ server-to-server, ghr_ refresh; github_pat_ fine-grained.
		{"github-token", `\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b`},
		{"github-fine-grained-pat", `\bgithub_pat_[A-Za-z0-9_]{82}\b`},

		// GitLab personal access token.
		{"gitlab-token", `\bglpat-[A-Za-z0-9_-]{20}\b`},

		// Slack bot / user / app tokens + incoming webhooks.
		{"slack-token", `\bxox[baprs]-[A-Za-z0-9-]{10,48}\b`},
		{"slack-webhook", `https://hooks\.slack\.com/services/T[A-Za-z0-9_]+/B[A-Za-z0-9_]+/[A-Za-z0-9_]+`},

		// Stripe live secret / restricted keys.
		{"stripe-key", `\b(?:sk|rk)_live_[A-Za-z0-9]{24,}\b`},

		// Google API key.
		{"google-api-key", `\bAIza[0-9A-Za-z_-]{35}\b`},

		// npm automation / publish token.
		{"npm-token", `\bnpm_[A-Za-z0-9]{36}\b`},

		// OpenAI / Anthropic-style provider keys.
		{"openai-key", `\bsk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}\b`},

		// PEM private keys (RSA / EC / DSA / OpenSSH / PGP / generic).
		{"private-key-pem", `-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`},

		// JWT — three base64url segments separated by dots, header
		// starting with the canonical {"alg" / {"typ JSON prefix.
		{"jwt", `\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`},

		// Credit-card numbers (Visa / Mastercard / Amex / Discover).
		// Higher false-positive rate than the token patterns — any
		// 13-16 digit run matching the issuer prefixes fires. Best-
		// effort; documented as such.
		{"credit-card", `\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`},

		// Generic high-confidence "secret"/"password"/"token" assignment
		// to a long opaque value. Context-anchored to keep noise down.
		{"generic-assignment", `(?i)(?:password|passwd|secret|token|api_?key)["'` + "`" + `]?\s*[:=]\s*["'` + "`" + `][A-Za-z0-9/+_=-]{16,}["'` + "`" + `]`},
	}
	out := make([]secretPattern, 0, len(specs))
	for _, s := range specs {
		out = append(out, secretPattern{kind: s.kind, re: regexp.MustCompile(s.expr)})
	}
	return out
}

// secretKindsCap bounds the returned category list. There are only ~15
// patterns so this is generous headroom; it exists to keep the wire
// shape bounded if the catalogue grows.
const secretKindsCap = 50

// DetectSecrets returns the sorted, de-duplicated list of secret
// categories that match anywhere in body. Empty slice when none match
// or body is empty. Exported for direct unit testing without going
// through CEL.
func DetectSecrets(body string) []string {
	if body == "" {
		return nil
	}
	seen := make(map[string]struct{})
	for _, p := range secretPatterns {
		if _, ok := seen[p.kind]; ok {
			continue
		}
		if p.re.MatchString(body) {
			seen[p.kind] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > secretKindsCap {
		out = out[:secretKindsCap]
	}
	return out
}

// HasSecrets reports whether body matches any secret pattern. Short-
// circuits on the first match — cheaper than DetectSecrets when only
// the boolean is needed.
func HasSecrets(body string) bool {
	if body == "" {
		return false
	}
	for _, p := range secretPatterns {
		if p.re.MatchString(body) {
			return true
		}
	}
	return false
}

// secretFunctions registers the has_secrets / secret_kinds CEL
// functions. Mirrors fuzzyFunctions / geoFunctions / imageFunctions —
// implement, declare here, add a FunctionDoc in schema.go.
func secretFunctions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("has_secrets",
			cel.Overload("has_secrets_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(hasSecretsBinding),
			),
		),
		cel.Function("secret_kinds",
			cel.Overload("secret_kinds_string",
				[]*cel.Type{cel.StringType},
				cel.ListType(cel.StringType),
				cel.UnaryBinding(secretKindsBinding),
			),
		),
	}
}

func hasSecretsBinding(arg ref.Val) ref.Val {
	body, ok := arg.Value().(string)
	if !ok {
		return types.False
	}
	return types.Bool(HasSecrets(body))
}

func secretKindsBinding(arg ref.Val) ref.Val {
	body, ok := arg.Value().(string)
	if !ok {
		return types.DefaultTypeAdapter.NativeToValue([]string{})
	}
	return types.DefaultTypeAdapter.NativeToValue(DetectSecrets(body))
}
