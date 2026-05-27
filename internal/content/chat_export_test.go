package content

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// --- Slack ---

const slackArrayFixture = `[
  {"type":"message","ts":"1609459200.000400","user":"U01","text":"deploying to kubernetes now","user_profile":{"real_name":"Alice Adams"}},
  {"type":"message","ts":"1609459260.000500","user":"U02","text":"thanks Alice","user_profile":{"name":"bob"}},
  {"type":"message","ts":"1609459320.000600","user":"U01","text":"rollout complete","user_profile":{"real_name":"Alice Adams"}}
]`

func TestSlackExport_Detection(t *testing.T) {
	fsys := fstest.MapFS{
		"acme/engineering/2021-01-01.json": {Data: []byte(slackArrayFixture)},
	}
	ct := DefaultRegistry().Detect(fsys, "acme/engineering/2021-01-01.json")
	if ct == nil || ct.Name() != "chat/slack-export" {
		t.Fatalf("Detect = %v, want chat/slack-export", ctName(ct))
	}
}

func TestSlackExport_Attributes(t *testing.T) {
	c := parseSlackExport([]byte(slackArrayFixture))
	attrs := c.toAttributes(dirName("acme/engineering/2021-01-01.json"), grandDirName("acme/engineering/2021-01-01.json"))

	if got := attrs["chat_message_count"]; got != int64(3) {
		t.Errorf("chat_message_count = %v, want 3", got)
	}
	if got := attrs["chat_channel"]; got != "engineering" {
		t.Errorf("chat_channel = %v, want engineering", got)
	}
	if got := attrs["chat_workspace"]; got != "acme" {
		t.Errorf("chat_workspace = %v, want acme", got)
	}
	parts, _ := attrs["chat_participants"].([]string)
	if len(parts) != 2 {
		t.Errorf("chat_participants = %v, want 2 distinct", parts)
	}
	if !containsStr(parts, "Alice Adams") || !containsStr(parts, "bob") {
		t.Errorf("participants = %v, want Alice Adams + bob", parts)
	}
}

func TestSlackExport_WrappedObjectForm(t *testing.T) {
	wrapped := `{"messages":` + slackArrayFixture + `}`
	fsys := fstest.MapFS{"chan.json": {Data: []byte(wrapped)}}
	ct := DefaultRegistry().Detect(fsys, "chan.json")
	if ct == nil || ct.Name() != "chat/slack-export" {
		t.Fatalf("wrapped-form Detect = %v, want chat/slack-export", ctName(ct))
	}
	c := parseSlackExport([]byte(wrapped))
	if c.count != 3 {
		t.Errorf("wrapped count = %d, want 3", c.count)
	}
}

func TestSlackExport_Body(t *testing.T) {
	body := chatBody(parseSlackExport([]byte(slackArrayFixture)), 1<<20)
	if !strings.Contains(body, "kubernetes") {
		t.Errorf("body should contain 'kubernetes': %q", body)
	}
	if !strings.Contains(body, "Alice Adams") {
		t.Errorf("body should carry the author: %q", body)
	}
}

// --- Discord ---

const discordFixture = `{
  "guild": {"id":"1","name":"My Server"},
  "channel": {"id":"2","name":"general"},
  "messageCount": 2,
  "messages": [
    {"id":"10","timestamp":"2021-03-01T10:00:00.000+00:00","author":{"name":"carol","nickname":"Caz"},"content":"anyone seen the kubernetes docs?"},
    {"id":"11","timestamp":"2021-03-01T10:05:00.000+00:00","author":{"name":"dave"},"content":"yep, link incoming"}
  ]
}`

func TestDiscordExport_Detection(t *testing.T) {
	fsys := fstest.MapFS{"My Server - general.json": {Data: []byte(discordFixture)}}
	ct := DefaultRegistry().Detect(fsys, "My Server - general.json")
	if ct == nil || ct.Name() != "chat/discord-export" {
		t.Fatalf("Detect = %v, want chat/discord-export", ctName(ct))
	}
}

func TestDiscordExport_Attributes(t *testing.T) {
	c, channel, guild := parseDiscordExport([]byte(discordFixture))
	attrs := c.toAttributes(channel, guild)
	if got := attrs["chat_message_count"]; got != int64(2) {
		t.Errorf("chat_message_count = %v, want 2", got)
	}
	if got := attrs["chat_channel"]; got != "general" {
		t.Errorf("chat_channel = %v, want general", got)
	}
	if got := attrs["chat_workspace"]; got != "My Server" {
		t.Errorf("chat_workspace = %v, want My Server", got)
	}
	parts, _ := attrs["chat_participants"].([]string)
	if !containsStr(parts, "Caz") || !containsStr(parts, "dave") {
		t.Errorf("participants = %v, want Caz (nickname) + dave", parts)
	}
	if attrs["chat_start_at"] == nil || attrs["chat_end_at"] == nil {
		t.Errorf("expected chat_start_at / chat_end_at to be populated")
	}
}

// --- Signal ---

const signalNDJSON = `{"envelope":{"source":"+15550001","sourceName":"Erin","timestamp":1609459200000,"dataMessage":{"message":"is the cluster up?"}}}
{"envelope":{"source":"+15550002","sourceName":"Frank","timestamp":1609459260000,"dataMessage":{"message":"kubernetes is green"}}}`

func TestSignalExport_Detection_NDJSON(t *testing.T) {
	fsys := fstest.MapFS{"+15550001.json": {Data: []byte(signalNDJSON)}}
	ct := DefaultRegistry().Detect(fsys, "+15550001.json")
	if ct == nil || ct.Name() != "chat/signal-cli" {
		t.Fatalf("Detect = %v, want chat/signal-cli", ctName(ct))
	}
}

func TestSignalExport_Attributes(t *testing.T) {
	c := parseSignalExport([]byte(signalNDJSON))
	attrs := c.toAttributes(signalContactFromPath("+15550001.json"), "")
	if got := attrs["chat_message_count"]; got != int64(2) {
		t.Errorf("chat_message_count = %v, want 2", got)
	}
	if got := attrs["chat_channel"]; got != "+15550001" {
		t.Errorf("chat_channel = %v, want +15550001", got)
	}
	if _, ok := attrs["chat_workspace"]; ok {
		t.Errorf("Signal should have no chat_workspace")
	}
	parts, _ := attrs["chat_participants"].([]string)
	if !containsStr(parts, "Erin") || !containsStr(parts, "Frank") {
		t.Errorf("participants = %v, want Erin + Frank", parts)
	}
}

func TestSignalExport_ArrayForm(t *testing.T) {
	arr := `[` + strings.ReplaceAll(signalNDJSON, "}}\n{", "}},{") + `]`
	c := parseSignalExport([]byte(arr))
	if c.count != 2 {
		t.Errorf("array-form count = %d, want 2", c.count)
	}
}

// --- discrimination boundary: a plain JSON object must NOT be claimed ---

func TestChatExport_PlainJSONNotClaimed(t *testing.T) {
	fsys := fstest.MapFS{"config.json": {Data: []byte(`{"name":"thing","version":2,"deps":["a","b"]}`)}}
	ct := DefaultRegistry().Detect(fsys, "config.json")
	if ct == nil || ct.Name() != "json" {
		t.Fatalf("plain JSON Detect = %v, want generic json", ctName(ct))
	}
}

func TestChatExport_DiscordNotSeenAsSlack(t *testing.T) {
	// Discord carries `messages` too, but guild+channel must route it
	// to discord, not slack.
	if (&slackExportType{}).MatchesContent([]byte(discordFixture)) {
		t.Error("Slack discriminator wrongly claimed a Discord export")
	}
	if !(&discordExportType{}).MatchesContent([]byte(discordFixture)) {
		t.Error("Discord discriminator failed to claim a Discord export")
	}
}

func ctName(ct ContentType) string {
	if ct == nil {
		return "<nil>"
	}
	return ct.Name()
}

func containsStr(ss []string, want string) bool {
	return slices.Contains(ss, want)
}
