package content

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

// chatCtxCheckMask sets how often the streaming JSON iterators poll
// ctx.Err(): every (mask+1) = 1024 elements. Cheap enough to be
// invisible on normal exports, frequent enough that a cancelled
// multi-million-message parse stops promptly.
const chatCtxCheckMask = 1<<10 - 1

// Chat-export content types (issue #214). Slack workspace exports,
// Discord (DiscordChatExporter) dumps, and signal-cli JSON are all
// plain `.json` files with no fixed basename, so they're detected by
// the ContentDiscriminator tier (top-level JSON shape) rather than by
// filename. All three collapse onto a shared message-level attribute
// surface: chat_message_count, chat_participants, chat_channel,
// chat_workspace, chat_start_at, chat_end_at — plus a `--body` body
// that joins messages as "{timestamp}\t{author}\t{text}" so
// body.contains("kubernetes") greps the conversation text.

const (
	// chatReadCap bounds disk reads per the issue's 100 MB defensive
	// ceiling. Real exports are typically a few MB; pathological dumps
	// degrade silently above the cap.
	chatReadCap = 100 << 20

	// chatMaxMessages caps how many messages are parsed (for body +
	// participant/time aggregation). Bounds CPU on adversarial input.
	chatMaxMessages = 10000

	// chatMaxParticipants caps the distinct-author list for a
	// predictable JSON wire shape.
	chatMaxParticipants = 500

	// chatSniffKeyCap bounds the streaming top-level-key sniff so a
	// pathological flat object can't make detection do unbounded work.
	chatSniffKeyCap = 64
)

// chatMessage is one normalized message across all three formats.
type chatMessage struct {
	ts     time.Time
	author string
	text   string
}

// chatCollector aggregates the message-level attribute surface. add()
// is fed normalized messages by each per-format parser; the parsed
// message list (for body extraction) is capped while count, participant
// dedup, and the time range track every add until chatMaxMessages.
type chatCollector struct {
	msgs         []chatMessage
	count        int64
	seen         map[string]struct{}
	participants []string
	startAt      time.Time
	endAt        time.Time
}

func (c *chatCollector) add(ts time.Time, author, text string) {
	c.count++
	author = strings.TrimSpace(author)
	if author != "" {
		if c.seen == nil {
			c.seen = make(map[string]struct{})
		}
		if _, ok := c.seen[author]; !ok && len(c.participants) < chatMaxParticipants {
			c.seen[author] = struct{}{}
			c.participants = append(c.participants, author)
		}
	}
	if !ts.IsZero() {
		if c.startAt.IsZero() || ts.Before(c.startAt) {
			c.startAt = ts
		}
		if c.endAt.IsZero() || ts.After(c.endAt) {
			c.endAt = ts
		}
	}
	c.msgs = append(c.msgs, chatMessage{ts: ts, author: author, text: text})
}

// full reports whether the collector has reached the message cap; parsers
// check this to stop streaming.
func (c *chatCollector) full() bool { return c.count >= chatMaxMessages }

// toAttributes assembles the CEL attribute map. channel / workspace are
// supplied by the per-format parser (from the file, or derived from the
// path for Slack / Signal). Empty / zero values are omitted so the
// zeroDefaults fallback supplies them.
func (c *chatCollector) toAttributes(channel, workspace string) Attributes {
	out := Attributes{"chat_message_count": c.count}
	if len(c.participants) > 0 {
		out["chat_participants"] = c.participants
	}
	if channel != "" {
		out["chat_channel"] = channel
	}
	if workspace != "" {
		out["chat_workspace"] = workspace
	}
	if !c.startAt.IsZero() {
		out["chat_start_at"] = c.startAt
	}
	if !c.endAt.IsZero() {
		out["chat_end_at"] = c.endAt
	}
	return out
}

// chatBody renders the collected messages as newline-separated
// "{RFC3339 timestamp}\t{author}\t{text}" lines, capped at maxBytes.
// Feeds body.contains(...) / body.matches(...).
func chatBody(c *chatCollector, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	var sb strings.Builder
	for _, m := range c.msgs {
		if sb.Len() >= maxBytes {
			break
		}
		if !m.ts.IsZero() {
			sb.WriteString(m.ts.UTC().Format(time.RFC3339))
		}
		sb.WriteByte('\t')
		sb.WriteString(m.author)
		sb.WriteByte('\t')
		sb.WriteString(strings.ReplaceAll(m.text, "\n", " "))
		sb.WriteByte('\n')
	}
	out := sb.String()
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}

// readChatFile reads the file head up to chatReadCap. Shared by every
// chat type's Attributes + body extractor. Returns empty bytes (not an
// error) on read failure so the walker contract holds.
func readChatFile(fsys fs.FS, p string) []byte {
	f, err := fsys.Open(p)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, chatReadCap))
	if err != nil {
		return nil
	}
	return buf
}

// --- streaming JSON shape sniffing (used by the discriminators) ---

// jsonShape is the minimal top-level structure needed to discriminate a
// chat export without parsing the whole file.
type jsonShape struct {
	isObject      bool
	isArray       bool
	objectKeys    map[string]bool // top-level keys when isObject
	firstElemKeys map[string]bool // first array element's keys when isArray
}

// sniffJSONShape reads just enough of head (via a streaming decoder) to
// classify the top-level value as an object (with its keys) or an array
// (with its first element's keys). ok is false when head isn't JSON.
func sniffJSONShape(head []byte) (jsonShape, bool) {
	dec := json.NewDecoder(bytes.NewReader(head))
	t, err := dec.Token()
	if err != nil {
		return jsonShape{}, false
	}
	d, ok := t.(json.Delim)
	if !ok {
		return jsonShape{}, false
	}
	switch d {
	case '{':
		return jsonShape{isObject: true, objectKeys: topLevelObjectKeys(dec)}, true
	case '[':
		return jsonShape{isArray: true, firstElemKeys: firstElementKeys(dec)}, true
	}
	return jsonShape{}, false
}

// topLevelObjectKeys collects the keys of the object the decoder is
// positioned inside (just past its opening `{`), skipping each value.
// Bounded by chatSniffKeyCap. Truncated input ends the walk gracefully.
func topLevelObjectKeys(dec *json.Decoder) map[string]bool {
	keys := make(map[string]bool)
	for dec.More() && len(keys) < chatSniffKeyCap {
		kt, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := kt.(string)
		if !ok {
			break
		}
		keys[key] = true
		if err := skipJSONValue(dec); err != nil {
			break
		}
	}
	return keys
}

// firstElementKeys returns the keys of the first element of the array the
// decoder is positioned inside (just past its opening `[`), when that
// element is an object. Returns nil for an empty array or a non-object
// first element.
func firstElementKeys(dec *json.Decoder) map[string]bool {
	if !dec.More() {
		return nil
	}
	t, err := dec.Token()
	if err != nil {
		return nil
	}
	d, ok := t.(json.Delim)
	if !ok || d != '{' {
		return nil
	}
	return topLevelObjectKeys(dec)
}

// skipJSONValue consumes exactly one JSON value (scalar or
// arbitrarily-nested object/array) from the decoder, tracking delimiter
// depth.
func skipJSONValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil // scalar value already consumed
	}
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}

// --- streaming element iteration (used by the parsers) ---

// forEachArrayElement streams the elements of a top-level JSON array,
// calling fn with each element's raw bytes until fn returns false, the
// array ends, or a decode error occurs. Does nothing when data isn't a
// top-level array.
func forEachArrayElement(ctx context.Context, data []byte, fn func(json.RawMessage) bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	t, err := dec.Token()
	if err != nil {
		return
	}
	if d, ok := t.(json.Delim); !ok || d != '[' {
		return
	}
	for n := 0; dec.More(); n++ {
		// A multi-million-element export must not run uninterruptibly
		// after a timeout / Ctrl-C — check ctx periodically (#321 audit).
		if n&chatCtxCheckMask == 0 && ctx.Err() != nil {
			return
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return
		}
		if !fn(raw) {
			return
		}
	}
}

// forEachJSONValue streams successive top-level JSON values from data —
// handles newline-delimited JSON (signal-cli's default output) as well
// as a single object. Calls fn with each value's raw bytes until fn
// returns false or input is exhausted.
func forEachJSONValue(ctx context.Context, data []byte, fn func(json.RawMessage) bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	for n := 0; ; n++ {
		if n&chatCtxCheckMask == 0 && ctx.Err() != nil {
			return // cancelled — stop streaming (#321 audit)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return
		}
		if !fn(raw) {
			return
		}
	}
}

// startsWithJSONArray reports whether the first non-whitespace byte of
// data is '['. Used to pick array vs value iteration for Signal dumps.
func startsWithJSONArray(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

// --- shared path / timestamp helpers ---

// dirName returns the basename of p's parent directory, or "" when p
// lives at the search root. Used to derive Slack channel / Signal
// contact names from the export's directory layout.
func dirName(p string) string {
	dir := path.Dir(p)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return path.Base(dir)
}

// fileStem returns the file's basename with its extension removed.
func fileStem(p string) string {
	base := path.Base(p)
	if ext := path.Ext(base); ext != "" {
		base = base[:len(base)-len(ext)]
	}
	return base
}

// grandDirName returns the basename of p's grandparent directory (the
// Slack workspace level in {workspace}/{channel}/file.json).
func grandDirName(p string) string {
	parent := path.Dir(p)
	if parent == "." || parent == "/" || parent == "" {
		return ""
	}
	return dirName(parent)
}

// parseSlackTS parses a Slack "ts" string ("1609459200.000400") into a
// time.Time. Returns the zero time on malformed input.
func parseSlackTS(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	sec := ts
	if before, _, ok := strings.Cut(ts, "."); ok {
		sec = before
	}
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}
