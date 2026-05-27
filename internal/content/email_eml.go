package content

import (
	"io/fs"
	"net/mail"
)

// readEMLMessage parses a single RFC 5322 message file and returns
// the unified email attribute surface. Streams via fsys.Open — no
// random access needed, mail.ReadMessage takes io.Reader.
func readEMLMessage(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	msg, err := mail.ReadMessage(f)
	if err != nil {
		return Attributes{}, nil // graceful degradation: malformed .eml returns empty attrs
	}
	return emailAttrs(msg), nil
}
