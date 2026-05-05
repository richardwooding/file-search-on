package content

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

// emailAttrs translates a parsed *mail.Message into the unified email
// attribute surface. Body is consumed for attachment counting (when
// the message is multipart) — caller must not reuse the message's
// Body reader afterwards.
func emailAttrs(msg *mail.Message) Attributes {
	attrs := Attributes{
		"title":             "",
		"author":            "",
		"email_to":          []string{},
		"email_cc":          []string{},
		"email_message_id":  "",
		"email_in_reply_to": "",
		"attachment_count":  int64(0),
	}

	if subj := decodeMIMEHeader(msg.Header.Get("Subject")); subj != "" {
		attrs["title"] = subj
	}
	if from := firstAddress(msg.Header, "From"); from != "" {
		attrs["author"] = from
	}
	if to := addressList(msg.Header, "To"); len(to) > 0 {
		attrs["email_to"] = to
	}
	if cc := addressList(msg.Header, "Cc"); len(cc) > 0 {
		attrs["email_cc"] = cc
	}
	if id := stripAngles(msg.Header.Get("Message-ID")); id != "" {
		attrs["email_message_id"] = id
	}
	if irt := stripAngles(msg.Header.Get("In-Reply-To")); irt != "" {
		attrs["email_in_reply_to"] = irt
	}
	if d, err := mail.ParseDate(msg.Header.Get("Date")); err == nil && !d.IsZero() {
		attrs["sent_at"] = d
	}
	if n := countAttachments(msg); n > 0 {
		attrs["attachment_count"] = int64(n)
	}

	return attrs
}

// firstAddress returns the display name of the first address in
// `header`, falling back to the address itself when no display name
// is set. Returns "" when the header is absent or unparseable.
func firstAddress(h mail.Header, key string) string {
	addrs, err := h.AddressList(key)
	if err != nil || len(addrs) == 0 {
		// Fall back to raw header value if address parsing fails —
		// real-world headers are messy and we'd rather surface
		// SOMETHING than drop the field entirely.
		return strings.TrimSpace(h.Get(key))
	}
	a := addrs[0]
	if a.Name != "" {
		return a.Name
	}
	return a.Address
}

// addressList returns the address-only forms of every parseable
// address in `header` — display names are dropped to keep the list
// shape predictable for `"alice@example.com" in email_to` queries.
func addressList(h mail.Header, key string) []string {
	addrs, err := h.AddressList(key)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return out
}

// stripAngles trims surrounding angle brackets from a header value.
// Used for Message-ID and In-Reply-To which are conventionally
// formatted as `<id@host>`.
func stripAngles(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}

// decodeMIMEHeader applies RFC 2047 encoded-word decoding to a header
// value. mail.ReadMessage already does this for address fields; for
// Subject we apply it explicitly. Returns the original string on any
// decode error.
func decodeMIMEHeader(s string) string {
	if s == "" {
		return s
	}
	dec := &mime.WordDecoder{}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// countAttachments counts top-level multipart parts that carry a
// `Content-Disposition: attachment` header or a `filename` parameter.
// For non-multipart messages, returns 0. Walks one level deep — nested
// multiparts are aggregated into their parent for the count.
func countAttachments(msg *mail.Message) int {
	ct := msg.Header.Get("Content-Type")
	if ct == "" {
		return 0
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return 0
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return 0
	}
	boundary := params["boundary"]
	if boundary == "" {
		return 0
	}
	mr := multipart.NewReader(msg.Body, boundary)
	count := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if isAttachmentPart(part) {
			count++
		}
		_ = part.Close()
	}
	return count
}

// isAttachmentPart returns true when a multipart sub-part looks like
// an attachment — explicit `Content-Disposition: attachment` or a
// `filename` parameter on either Content-Disposition or Content-Type.
func isAttachmentPart(p *multipart.Part) bool {
	cd := p.Header.Get("Content-Disposition")
	if cd != "" {
		dispType, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if strings.EqualFold(dispType, "attachment") {
				return true
			}
			if params["filename"] != "" {
				return true
			}
		}
	}
	ct := p.Header.Get("Content-Type")
	if ct != "" {
		_, params, err := mime.ParseMediaType(ct)
		if err == nil && params["name"] != "" {
			return true
		}
	}
	return false
}
