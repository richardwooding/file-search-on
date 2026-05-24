package content

import (
	"bytes"
	"testing"
	"time"

	"howett.net/plist"
)

func TestParseQuarantineValue_FullFiveField(t *testing.T) {
	// Real shape from a Safari download — hex timestamp 0x69c554fd is
	// 2026-03-26T15:47:09Z, flags 0x0083 has the user-approved bit.
	value := "0083;69c554fd;Safari;A8E5945D-F07B-4D14-8523-867ABAA3B19F;https://example.com/file.zip"

	flags, downloadTime, agent, eventID, sourceURL := parseQuarantineValue(value)

	if flags != 0x0083 {
		t.Errorf("flags = %#x, want 0x0083", flags)
	}
	if got := downloadTime.UTC().Format(time.RFC3339); got != "2026-03-26T15:47:09Z" {
		t.Errorf("downloadTime = %s, want 2026-03-26T15:47:09Z", got)
	}
	if agent != "Safari" {
		t.Errorf("agent = %q", agent)
	}
	if eventID != "A8E5945D-F07B-4D14-8523-867ABAA3B19F" {
		t.Errorf("eventID = %q", eventID)
	}
	if sourceURL != "https://example.com/file.zip" {
		t.Errorf("sourceURL = %q", sourceURL)
	}
}

func TestParseQuarantineValue_FourFieldNoURL(t *testing.T) {
	// Modern Safari downloads typically don't include the URL in the
	// quarantine string itself — it's in the separate WhereFroms xattr.
	value := "0083;69c554fd;Safari;UUID-HERE"
	_, _, agent, eventID, sourceURL := parseQuarantineValue(value)
	if agent != "Safari" {
		t.Errorf("agent = %q", agent)
	}
	if eventID != "UUID-HERE" {
		t.Errorf("eventID = %q", eventID)
	}
	if sourceURL != "" {
		t.Errorf("sourceURL should be empty, got %q", sourceURL)
	}
}

func TestParseQuarantineValue_URLWithSemicolon(t *testing.T) {
	// URLs with `;` in query strings — SplitN(n=5) preserves the URL.
	value := "0083;0;Mail;com.apple.Mail;https://example.com/a?x=1;y=2;z=3"
	_, _, _, _, sourceURL := parseQuarantineValue(value)
	want := "https://example.com/a?x=1;y=2;z=3"
	if sourceURL != want {
		t.Errorf("sourceURL = %q, want %q", sourceURL, want)
	}
}

func TestParseQuarantineValue_EmptyString(t *testing.T) {
	flags, downloadTime, agent, eventID, sourceURL := parseQuarantineValue("")
	if flags != 0 || !downloadTime.IsZero() || agent != "" || eventID != "" || sourceURL != "" {
		t.Errorf("expected all zero values for empty input")
	}
}

func TestParseQuarantineValue_MalformedFlags(t *testing.T) {
	value := "not-hex;69c554fd;Safari;abc"
	flags, downloadTime, agent, _, _ := parseQuarantineValue(value)
	if flags != 0 {
		t.Errorf("flags should be 0 on parse failure, got %#x", flags)
	}
	if downloadTime.IsZero() {
		t.Error("downloadTime should still parse")
	}
	if agent != "Safari" {
		t.Errorf("agent = %q", agent)
	}
}

func TestMergeQuarantineAttrs_UserApprovedBit(t *testing.T) {
	out := Attributes{}
	mergeQuarantineAttrs(out, []byte("0083;0;Safari;uuid"))
	if out["is_quarantined"] != true {
		t.Error("is_quarantined should be true")
	}
	if out["quarantine_user_approved"] != true {
		t.Error("quarantine_user_approved should be true (flag 0x80 set)")
	}
}

func TestMergeQuarantineAttrs_NotApproved(t *testing.T) {
	out := Attributes{}
	mergeQuarantineAttrs(out, []byte("0001;0;Safari;uuid"))
	if out["is_quarantined"] != true {
		t.Error("is_quarantined should be true (xattr present)")
	}
	if _, ok := out["quarantine_user_approved"]; ok {
		t.Error("quarantine_user_approved should be absent (flag bits not set)")
	}
}

func TestMergeWhereFromsAttrs_URLAndReferrer(t *testing.T) {
	root := []any{
		"https://example.com/download.zip",
		"https://example.com/page-that-linked-it",
	}
	var buf bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf, plist.BinaryFormat).Encode(root)

	out := Attributes{}
	mergeWhereFromsAttrs(out, buf.Bytes())

	if got := out["quarantine_source_url"]; got != "https://example.com/download.zip" {
		t.Errorf("quarantine_source_url = %v", got)
	}
	if got := out["quarantine_referrer_url"]; got != "https://example.com/page-that-linked-it" {
		t.Errorf("quarantine_referrer_url = %v", got)
	}
}

func TestMergeFinderTagAttrs_ColorAndUserTags(t *testing.T) {
	root := []any{
		"\n6",       // red color tag
		"\n3",       // purple color tag
		"work-2026", // user tag
		"important", // user tag
	}
	var buf bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf, plist.BinaryFormat).Encode(root)

	out := Attributes{}
	mergeFinderTagAttrs(out, buf.Bytes())

	if got := out["finder_color"]; got != "red" {
		t.Errorf("finder_color = %v, want red (first color wins)", got)
	}
	tags, ok := out["finder_tags"].([]string)
	if !ok || len(tags) != 2 {
		t.Fatalf("finder_tags = %v, want 2 user tags", tags)
	}
	// Tags are sorted; "important" < "work-2026" lexically.
	if tags[0] != "important" || tags[1] != "work-2026" {
		t.Errorf("finder_tags = %v, want [important, work-2026]", tags)
	}
}

func TestMergeFinderTagAttrs_OnlyUserTags(t *testing.T) {
	root := []any{"alpha", "beta"}
	var buf bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf, plist.BinaryFormat).Encode(root)

	out := Attributes{}
	mergeFinderTagAttrs(out, buf.Bytes())

	if _, ok := out["finder_color"]; ok {
		t.Error("finder_color should be absent (no color tags)")
	}
	tags, _ := out["finder_tags"].([]string)
	if len(tags) != 2 {
		t.Errorf("finder_tags = %v, want 2", tags)
	}
}
