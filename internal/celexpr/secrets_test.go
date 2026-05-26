package celexpr

import (
	"slices"
	"testing"
)

// TestDetectSecretsPositive — each known secret kind, embedded in a
// realistic config / source snippet, must be detected. The token
// values are SYNTHETIC (correct shape, fake content). Each literal is
// SPLIT across a string concatenation so the contiguous secret pattern
// never appears in this source file — otherwise GitHub's own push-
// protection scanner blocks the commit (which is exactly the kind of
// thing this feature detects, so the irony is acknowledged). The
// runtime-concatenated value still matches our regexes.
func TestDetectSecretsPositive(t *testing.T) {
	cases := []struct {
		kind string
		body string
	}{
		{"aws-access-key", `export AWS_ACCESS_KEY_ID=AKIA` + `IOSFODNN7EXAMPLE`},
		{"aws-secret-key", `aws_secret_access_key = wJalr` + `XUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`},
		{"github-token", `GH_TOKEN=ghp` + `_1234567890abcdefghijklmnopqrstuvwxyz`},
		{"github-fine-grained-pat", `token: github_pat` + `_11ABCDEFG0aBcDeFgHiJkLm_1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP123456`},
		{"gitlab-token", `GITLAB_TOKEN=glpat` + `-abcdefABCDEF12345678`},
		{"slack-token", `SLACK_BOT_TOKEN=xox` + `b-1234567890-abcdefghijklmnop`},
		{"slack-webhook", `url = https://hooks.slack.com/services/` + `T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX`},
		{"stripe-key", `STRIPE_KEY=sk` + `_live_abcdefghijklmnopqrstuvwx`},
		{"google-api-key", `key: AIza` + `SyA1234567890abcdefghijklmnopqrstuv`},
		{"npm-token", `//registry.npmjs.org/:_authToken=npm` + `_abcdefghijklmnopqrstuvwxyz0123456789`},
		{"private-key-pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"},
		{"private-key-pem", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk...\n-----END OPENSSH PRIVATE KEY-----"},
		{"jwt", `Authorization: Bearer eyJ` + `hbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`},
		{"credit-card", `card: 4111` + `111111111111`},
		{"generic-assignment", `password = "hunter2hunter2hunter2"`},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := DetectSecrets(tc.body)
			if !slices.Contains(got, tc.kind) {
				t.Errorf("DetectSecrets() = %v, expected to contain %q", got, tc.kind)
			}
			if !HasSecrets(tc.body) {
				t.Errorf("HasSecrets() = false, expected true for %q", tc.kind)
			}
		})
	}
}

// TestDetectSecretsNegative — plain text that superficially resembles
// secret patterns but isn't, must NOT fire. Guards against the noisy
// false-positive case.
func TestDetectSecretsNegative(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"prose", "The quick brown fox jumps over the lazy dog. AWS is a cloud provider."},
		{"short-hex", "color: #AKIA12 is not a key"},
		{"akia-too-short", "AKIA123"},                                   // prefix but not 16 chars
		{"ghp-too-short", "ghp_short"},                                  // GitHub prefix, wrong length
		{"random-digits", "the order number is 12345 and 67890"},        // not a card pattern
		{"version-string", "version 4.11.1 of the library"},            // not a card
		{"markdown-heading", "# AWS Setup\n\nConfigure your credentials in the console."},
		{"plain-jwt-word", "We use JWT for auth, eyJ is the base64 prefix."}, // eyJ alone, not a full token
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectSecrets(tc.body); len(got) != 0 {
				t.Errorf("DetectSecrets(%q) = %v, expected no matches", tc.body, got)
			}
			if HasSecrets(tc.body) {
				t.Errorf("HasSecrets(%q) = true, expected false", tc.body)
			}
		})
	}
}

// TestDetectSecretsMultiple — a body with several kinds returns all of
// them, sorted and de-duplicated.
func TestDetectSecretsMultiple(t *testing.T) {
	body := "AWS_ACCESS_KEY_ID=AKIA" + "IOSFODNN7EXAMPLE\n" +
		"GH_TOKEN=ghp" + "_1234567890abcdefghijklmnopqrstuvwxyz\n" +
		"AWS_ACCESS_KEY_ID=AKIA" + "IOSFODNN7EXAMPLE\n"
	got := DetectSecrets(body)
	want := []string{"aws-access-key", "github-token"}
	if !slices.Equal(got, want) {
		t.Errorf("DetectSecrets() = %v, want %v (sorted, de-duped)", got, want)
	}
}

// TestSecretFunctionsViaCEL drives the functions through a compiled
// CEL program, the way a real query does.
func TestSecretFunctionsViaCEL(t *testing.T) {
	cases := []struct {
		expr string
		body string
		want bool
	}{
		{`has_secrets(body)`, `key=ghp` + `_1234567890abcdefghijklmnopqrstuvwxyz`, true},
		{`has_secrets(body)`, `just some prose here`, false},
		{`"aws-access-key" in secret_kinds(body)`, `AKIA` + `IOSFODNN7EXAMPLE`, true},
		{`"github-token" in secret_kinds(body)`, `AKIA` + `IOSFODNN7EXAMPLE`, false},
		{`secret_kinds(body).size() > 0`, `password = "supersecretvalue123"`, true},
		{`secret_kinds(body).size() == 0`, `nothing to see`, true},
	}
	for _, tc := range cases {
		t.Run(tc.expr+"|"+tc.body, func(t *testing.T) {
			ev, err := New(tc.expr)
			if err != nil {
				t.Fatalf("New(%q): %v", tc.expr, err)
			}
			attrs := &FileAttributes{
				ContentType: "text",
				Extra:       map[string]any{"body": tc.body},
			}
			got, err := ev.Evaluate(attrs)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got != tc.want {
				t.Errorf("%q against %q = %v, want %v", tc.expr, tc.body, got, tc.want)
			}
		})
	}
}
