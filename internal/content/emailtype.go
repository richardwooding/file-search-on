package content

import (
	"context"
	"errors"
	"io/fs"
)

func init() {
	Register(&emailType{
		name:  "email/rfc822",
		exts:  []string{".eml", ".email"},
		magic: nil,
	})
	Register(&emailType{
		name:  "email/mbox",
		exts:  []string{".mbox"},
		magic: [][]byte{{'F', 'r', 'o', 'm', ' '}},
	})
}

type emailType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (e *emailType) Name() string         { return e.name }
func (e *emailType) Extensions() []string { return e.exts }
func (e *emailType) MagicBytes() [][]byte { return e.magic }

// Attributes dispatches to the per-format email parser. Both paths
// produce the same surface — title, author, email_to, email_cc,
// email_message_id, email_in_reply_to, sent_at, attachment_count —
// plus email_count for mbox archives. mbox parsers reflect the FIRST
// message's headers; email_count carries the multi-message shape.
func (e *emailType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch e.name {
	case "email/rfc822":
		return readEMLMessage(fsys, path)
	case "email/mbox":
		return readMBOXArchive(fsys, path)
	}
	return nil, errors.New("unsupported email type")
}
