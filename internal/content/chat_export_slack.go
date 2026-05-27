package content

import (
	"context"
	"encoding/json"
	"io/fs"
)

// Slack workspace exports ship one JSON file per channel per day at
// {workspace}/{channel}/YYYY-MM-DD.json. The channel-day file is a
// top-level ARRAY of message objects; some tooling wraps it as
// {"messages": [...]}. Both forms are supported. Channel + workspace
// names aren't in the file (the file is named by date), so they're
// derived from the export's directory layout.
//
// Message shape: {"type":"message","ts":"1609459200.000400","user":"U…",
// "text":"…","user_profile":{"name":"…","real_name":"…"}}.

func init() { Register(&slackExportType{}) }

type slackExportType struct{}

func (*slackExportType) Name() string                      { return "chat/slack-export" }
func (*slackExportType) Extensions() []string              { return nil }
func (*slackExportType) MagicBytes() [][]byte              { return nil }
func (*slackExportType) DiscriminatorExtensions() []string { return []string{".json"} }

// MatchesContent claims the file when it's a top-level array of Slack
// messages (first element has `ts` plus one of type/user/text, and no
// `envelope` — which would be Signal), or the wrapped object form
// {"messages": [...]} without Discord's guild/channel keys.
func (*slackExportType) MatchesContent(head []byte) bool {
	shape, ok := sniffJSONShape(head)
	if !ok {
		return false
	}
	if shape.isArray {
		k := shape.firstElemKeys
		if k == nil || k["envelope"] {
			return false
		}
		return k["ts"] && (k["type"] || k["user"] || k["text"])
	}
	if shape.isObject {
		o := shape.objectKeys
		return o["messages"] && !o["guild"] && !o["channel"]
	}
	return false
}

func (*slackExportType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return Attributes{}, nil
	}
	c := parseSlackExport(buf)
	return c.toAttributes(dirName(p), grandDirName(p)), nil
}

type slackMessage struct {
	Type        string `json:"type"`
	TS          string `json:"ts"`
	User        string `json:"user"`
	Text        string `json:"text"`
	UserProfile struct {
		Name     string `json:"name"`
		RealName string `json:"real_name"`
	} `json:"user_profile"`
}

func (m *slackMessage) author() string {
	switch {
	case m.UserProfile.RealName != "":
		return m.UserProfile.RealName
	case m.UserProfile.Name != "":
		return m.UserProfile.Name
	default:
		return m.User
	}
}

// parseSlackExport streams the message array (array form) or the
// wrapped object's `messages` array, collecting the shared chat surface.
// Pure function — fuzz target exercises it directly.
func parseSlackExport(data []byte) *chatCollector {
	c := &chatCollector{}
	consume := func(raw json.RawMessage) bool {
		var m slackMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return true // skip malformed element, keep going
		}
		c.add(parseSlackTS(m.TS), m.author(), m.Text)
		return !c.full()
	}
	if startsWithJSONArray(data) {
		forEachArrayElement(data, consume)
		return c
	}
	// Wrapped object form: {"messages": [...]}.
	var wrapper struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil || len(wrapper.Messages) == 0 {
		return c
	}
	forEachArrayElement(wrapper.Messages, consume)
	return c
}

func slackExportBody(ctx context.Context, fsys fs.FS, p string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return "", nil
	}
	return chatBody(parseSlackExport(buf), maxBytes), nil
}
