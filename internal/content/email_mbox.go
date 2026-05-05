package content

import (
	"bufio"
	"bytes"
	"io/fs"
	"maps"
	"net/mail"
	"strings"
)

// readMBOXArchive parses an mbox file (RFC 4155-ish; tolerates all
// real-world dialects since we key on `^From ` separators alone).
// Returns the first message's attributes plus an `email_count` of
// the total messages in the archive.
//
// mbox separator convention: every message starts with a line whose
// first 5 bytes are `From ` (note the trailing space, NOT a colon).
// We split on those line starts and parse the first slice with
// net/mail. Subsequent messages contribute only to email_count.
func readMBOXArchive(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	var firstMsg bytes.Buffer
	count := int64(0)
	inFirst := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if isMboxSeparator(line) {
			count++
			if count == 1 {
				inFirst = true
				continue // skip the separator itself; mail.ReadMessage doesn't want it
			}
			inFirst = false
		}
		if inFirst {
			firstMsg.Write(line)
			firstMsg.WriteByte('\n')
		}
	}

	attrs := Attributes{
		"title":             "",
		"author":            "",
		"email_to":          []string{},
		"email_cc":          []string{},
		"email_message_id":  "",
		"email_in_reply_to": "",
		"attachment_count":  int64(0),
		"email_count":       count,
	}
	if firstMsg.Len() == 0 {
		return attrs, nil
	}
	msg, err := mail.ReadMessage(&firstMsg)
	if err != nil {
		return attrs, nil //nolint:nilerr // graceful degradation: keep email_count, drop per-message attrs
	}
	first := emailAttrs(msg)
	maps.Copy(attrs, first)
	attrs["email_count"] = count
	return attrs, nil
}

// isMboxSeparator returns true when a line opens a new mbox message:
// starts with `From ` (5 bytes — capital F, lowercase rom, space).
// Body text that legitimately begins with `From ` is conventionally
// escaped to `>From ` (mboxrd) — we don't need to handle that here
// since we look for the unescaped form only.
func isMboxSeparator(line []byte) bool {
	if len(line) < 5 {
		return false
	}
	return bytes.HasPrefix(line, []byte("From ")) && plausibleMboxFromLine(line)
}

// plausibleMboxFromLine checks that a `From ` line looks like a real
// separator and not body text that happens to start the same way.
// Real separators carry `From <sender> <date>` — at minimum, an `@`
// (sender address) and at least one space after the leading `From `.
func plausibleMboxFromLine(line []byte) bool {
	rest := string(line[len("From "):])
	if !strings.Contains(rest, "@") {
		return false
	}
	return strings.Contains(rest, " ")
}
