package content

import (
	"context"
	"encoding/json"
	"io/fs"
	"time"
)

// signal-cli `--json` output is a stream of envelope objects — newline-
// delimited by default, sometimes wrapped in a top-level array:
//
//	{"envelope":{"source":"+15551234","sourceName":"Alice",
//	             "timestamp":1609459200000,
//	             "dataMessage":{"message":"hi"}}}
//
// There's no workspace concept; chat_channel is derived from the file
// basename (typically the contact / number the dump is named after).

func init() { Register(&signalExportType{}) }

type signalExportType struct{}

func (*signalExportType) Name() string                      { return "chat/signal-cli" }
func (*signalExportType) Extensions() []string              { return nil }
func (*signalExportType) MagicBytes() [][]byte              { return nil }
func (*signalExportType) DiscriminatorExtensions() []string { return []string{".json"} }

// MatchesContent claims the file when the top-level object carries an
// `envelope` key (NDJSON first object) or the top-level array's first
// element does (wrapped form).
func (*signalExportType) MatchesContent(head []byte) bool {
	shape, ok := sniffJSONShape(head)
	if !ok {
		return false
	}
	if shape.isObject {
		return shape.objectKeys["envelope"]
	}
	if shape.isArray {
		return shape.firstElemKeys != nil && shape.firstElemKeys["envelope"]
	}
	return false
}

func (*signalExportType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return Attributes{}, nil
	}
	c := parseSignalExport(buf)
	// chat_channel = contact (file basename without extension); no
	// workspace concept for Signal.
	channel := signalContactFromPath(p)
	return c.toAttributes(channel, ""), nil
}

type signalEnvelope struct {
	Envelope struct {
		Source      string `json:"source"`
		SourceName  string `json:"sourceName"`
		Timestamp   int64  `json:"timestamp"`
		DataMessage struct {
			Message string `json:"message"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

func (e *signalEnvelope) author() string {
	if e.Envelope.SourceName != "" {
		return e.Envelope.SourceName
	}
	return e.Envelope.Source
}

// parseSignalExport streams envelope objects from either the NDJSON or
// the wrapped-array form. Pure function — fuzz target exercises it
// directly.
func parseSignalExport(data []byte) *chatCollector {
	c := &chatCollector{}
	consume := func(raw json.RawMessage) bool {
		var e signalEnvelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return true
		}
		var ts time.Time
		if e.Envelope.Timestamp > 0 {
			ts = time.UnixMilli(e.Envelope.Timestamp).UTC()
		}
		c.add(ts, e.author(), e.Envelope.DataMessage.Message)
		return !c.full()
	}
	if startsWithJSONArray(data) {
		forEachArrayElement(data, consume)
	} else {
		forEachJSONValue(data, consume)
	}
	return c
}

// signalContactFromPath returns the file basename without its .json
// extension — signal-cli dumps are typically named after the contact.
func signalContactFromPath(p string) string {
	return fileStem(p)
}

func signalExportBody(ctx context.Context, fsys fs.FS, p string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return "", nil
	}
	return chatBody(parseSignalExport(buf), maxBytes), nil
}
