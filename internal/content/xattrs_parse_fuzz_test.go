package content

import "testing"

// FuzzParseQuarantineValue targets the semicolon-delimited quarantine
// parser. The format is loose enough that adversarial mutators tend
// to find adjacent issues — non-hex flag fields, partial UTF-8,
// embedded semicolons that should still be preserved in the URL.
//
// Cross-platform — parser is pure string manipulation, no darwin
// syscalls. Build-tagged darwin code lives in xattrs_darwin.go.
//
// Contract: never panic.
func FuzzParseQuarantineValue(f *testing.F) {
	f.Add("0083;69c554fd;Safari;UUID-HERE;https://example.com/file.zip")
	f.Add("0083;0;Mail;com.apple.Mail;mailto:test@example.com")
	f.Add("0001;abcdef;TestAgent;test;")
	f.Add("")
	f.Add(";;;;")
	f.Add("not-hex;not-hex;;;")
	// URL with multiple semicolons (modern Google Docs export URLs do this).
	f.Add("0083;69c554fd;Safari;uuid;https://example.com/a?x=1;y=2;z=3;w=4")
	// Truncated mid-field.
	f.Add("0083;69c554")

	f.Fuzz(func(t *testing.T, value string) {
		// Shape contract — no panic. Return values are unconstrained
		// beyond "must not panic"; the merge functions are responsible
		// for sane defaults on garbage input.
		_, _, _, _, _ = parseQuarantineValue(value)
	})
}
