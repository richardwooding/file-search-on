package content_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

const minimalEML = `From: alice@example.com
To: bob@example.com
Subject: Minimal test
Date: Tue, 14 Apr 2026 09:30:00 +0000
Message-ID: <min-001@example.com>

Body of the minimal email.
`

const multipartEML = `From: "Alice" <alice@example.com>
To: "Bob" <bob@example.com>, charlie@example.com
Cc: qa@example.com
Subject: Multipart test
Date: Tue, 14 Apr 2026 09:30:00 +0000
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b42"

--b42
Content-Type: text/plain

Body part.

--b42
Content-Type: application/octet-stream; name="data.bin"
Content-Disposition: attachment; filename="data.bin"

binary content placeholder

--b42--
`

const twoMessageMBOX = `From alice@example.com Tue Apr 14 09:30:00 2026
From: alice@example.com
To: bob@example.com
Subject: First
Date: Tue, 14 Apr 2026 09:30:00 +0000
Message-ID: <a@example.com>

First body.

From bob@example.com Tue Apr 14 10:15:00 2026
From: bob@example.com
To: alice@example.com
Subject: Second
Date: Tue, 14 Apr 2026 10:15:00 +0000
Message-ID: <b@example.com>

Second body.
`

func TestEmailEML_Minimal(t *testing.T) {
	fsys := fstest.MapFS{"a.eml": {Data: []byte(minimalEML)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.eml")
	if ct == nil || ct.Name() != "email/rfc822" {
		t.Fatalf("Detect = %v; want email/rfc822", ct)
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.eml")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if a["title"] != "Minimal test" {
		t.Errorf("title = %q; want %q", a["title"], "Minimal test")
	}
	if a["author"] != "alice@example.com" {
		t.Errorf("author = %q; want alice@example.com", a["author"])
	}
	to, _ := a["email_to"].([]string)
	if len(to) != 1 || to[0] != "bob@example.com" {
		t.Errorf("email_to = %v; want [bob@example.com]", to)
	}
	if a["email_message_id"] != "min-001@example.com" {
		t.Errorf("email_message_id = %q; want min-001@example.com", a["email_message_id"])
	}
	if d, ok := a["sent_at"].(time.Time); !ok || d.IsZero() {
		t.Errorf("sent_at = %v; want non-zero time", a["sent_at"])
	}
	if ac, _ := a["attachment_count"].(int64); ac != 0 {
		t.Errorf("attachment_count = %v; want 0 for non-multipart", a["attachment_count"])
	}
}

func TestEmailEML_Multipart(t *testing.T) {
	fsys := fstest.MapFS{"a.eml": {Data: []byte(multipartEML)}}
	a, err := content.DefaultRegistry().Detect(fsys, "a.eml").Attributes(t.Context(), fsys, "a.eml")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if a["author"] != "Alice" {
		t.Errorf("author = %q; want Alice (display name)", a["author"])
	}
	to, _ := a["email_to"].([]string)
	if len(to) != 2 {
		t.Errorf("email_to = %v; want 2 entries", to)
	}
	cc, _ := a["email_cc"].([]string)
	if len(cc) != 1 || cc[0] != "qa@example.com" {
		t.Errorf("email_cc = %v; want [qa@example.com]", cc)
	}
	if ac, _ := a["attachment_count"].(int64); ac != 1 {
		t.Errorf("attachment_count = %v; want 1", a["attachment_count"])
	}
}

func TestEmailMBOX_Detection(t *testing.T) {
	fsys := fstest.MapFS{"a.mbox": {Data: []byte(twoMessageMBOX)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.mbox")
	if ct == nil || ct.Name() != "email/mbox" {
		t.Fatalf("Detect = %v; want email/mbox", ct)
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.mbox")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if c, _ := a["email_count"].(int64); c != 2 {
		t.Errorf("email_count = %v; want 2", a["email_count"])
	}
	// First message attributes should leak through.
	if a["title"] != "First" {
		t.Errorf("title = %q; want First (from first message)", a["title"])
	}
	if a["email_message_id"] != "a@example.com" {
		t.Errorf("email_message_id = %q; want a@example.com", a["email_message_id"])
	}
}

func TestEmailMBOX_DetectByMagic(t *testing.T) {
	// .mbx extension isn't registered; rely on the `From ` magic at offset 0.
	body := []byte(twoMessageMBOX)
	fsys := fstest.MapFS{"archive.dat": {Data: body}}
	ct := content.DefaultRegistry().Detect(fsys, "archive.dat")
	if ct == nil {
		t.Fatalf("Detect returned nil for mbox-with-no-extension")
	}
	if ct.Name() != "email/mbox" {
		t.Errorf("Detect.Name() = %q; want email/mbox via magic-byte fallback", ct.Name())
	}
}

func TestEmailEML_StripsAngleBrackets(t *testing.T) {
	src := []byte("From: alice@example.com\r\nMessage-ID: <id-with-angles@example.com>\r\nIn-Reply-To: <prior@example.com>\r\n\r\nbody\r\n")
	fsys := fstest.MapFS{"a.eml": {Data: src}}
	a, err := content.DefaultRegistry().Detect(fsys, "a.eml").Attributes(t.Context(), fsys, "a.eml")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if a["email_message_id"] != "id-with-angles@example.com" {
		t.Errorf("email_message_id = %q; want angles stripped", a["email_message_id"])
	}
	if a["email_in_reply_to"] != "prior@example.com" {
		t.Errorf("email_in_reply_to = %q; want angles stripped", a["email_in_reply_to"])
	}
}
