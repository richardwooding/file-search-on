package content

import (
	"context"
	"encoding/json"
	"io/fs"
	"time"
)

// Discord exports from DiscordChatExporter (JSON mode) are a single
// object carrying guild + channel metadata and a messages array:
//
//	{
//	  "guild":    {"id":"…","name":"My Server"},
//	  "channel":  {"id":"…","name":"general","category":"…"},
//	  "messages": [{"id":"…","timestamp":"2021-01-01T00:00:00.000+00:00",
//	                "author":{"name":"…","nickname":"…"},"content":"…"}],
//	  "messageCount": N
//	}
//
// channel name → chat_channel, guild name → chat_workspace.

func init() { Register(&discordExportType{}) }

type discordExportType struct{}

func (*discordExportType) Name() string                      { return "chat/discord-export" }
func (*discordExportType) Extensions() []string              { return nil }
func (*discordExportType) MagicBytes() [][]byte              { return nil }
func (*discordExportType) DiscriminatorExtensions() []string { return []string{".json"} }

// MatchesContent claims the file when the top-level object carries the
// DiscordChatExporter triad guild + channel + messages.
func (*discordExportType) MatchesContent(head []byte) bool {
	shape, ok := sniffJSONShape(head)
	if !ok || !shape.isObject {
		return false
	}
	o := shape.objectKeys
	return o["guild"] && o["channel"] && o["messages"]
}

func (*discordExportType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return Attributes{}, nil
	}
	c, channel, guild := parseDiscordExport(ctx, buf)
	return c.toAttributes(channel, guild), nil
}

type discordMessage struct {
	Timestamp string `json:"timestamp"`
	Content   string `json:"content"`
	Author    struct {
		Name     string `json:"name"`
		Nickname string `json:"nickname"`
	} `json:"author"`
}

func (m *discordMessage) author() string {
	if m.Author.Nickname != "" {
		return m.Author.Nickname
	}
	return m.Author.Name
}

// parseDiscordExport extracts guild + channel names and streams the
// messages array. Pure function — fuzz target exercises it directly.
func parseDiscordExport(ctx context.Context, data []byte) (c *chatCollector, channel, guild string) {
	c = &chatCollector{}
	var file struct {
		Guild    struct{ Name string `json:"name"` } `json:"guild"`
		Channel  struct{ Name string `json:"name"` } `json:"channel"`
		Messages json.RawMessage                     `json:"messages"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return c, "", ""
	}
	forEachArrayElement(ctx, file.Messages, func(raw json.RawMessage) bool {
		var m discordMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return true
		}
		var ts time.Time
		if t, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
			ts = t
		}
		c.add(ts, m.author(), m.Content)
		return !c.full()
	})
	return c, file.Channel.Name, file.Guild.Name
}

func discordExportBody(ctx context.Context, fsys fs.FS, p string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	buf := readChatFile(fsys, p)
	if len(buf) == 0 {
		return "", nil
	}
	c, _, _ := parseDiscordExport(ctx, buf)
	return chatBody(c, maxBytes), nil
}
