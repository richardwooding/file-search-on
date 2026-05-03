package content

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Frontmatter is a parsed front-matter block.
type Frontmatter struct {
	Format string         // "yaml", "toml", "json"
	Data   map[string]any // parsed metadata
}

// splitFrontmatter splits a markdown file's bytes into the front-matter (if
// any) and the remaining body. If no recognised front-matter is found, fm is
// nil and body is the original input.
func splitFrontmatter(data []byte) (fm *Frontmatter, body []byte) {
	if len(data) == 0 {
		return nil, data
	}
	switch {
	case bytes.HasPrefix(data, []byte("---\n")), bytes.HasPrefix(data, []byte("---\r\n")):
		return parseDelimited(data, "---", "yaml", yaml.Unmarshal)
	case bytes.HasPrefix(data, []byte("+++\n")), bytes.HasPrefix(data, []byte("+++\r\n")):
		return parseDelimited(data, "+++", "toml", toml.Unmarshal)
	case data[0] == '{':
		return parseJSONFrontmatter(data)
	}
	return nil, data
}

func parseDelimited(data []byte, delim, format string, unmarshal func([]byte, any) error) (*Frontmatter, []byte) {
	// Skip the opening delimiter line.
	_, rest, ok := bytes.Cut(data, []byte("\n"))
	if !ok {
		return nil, data
	}
	// Find the closing delimiter on its own line.
	delimLine := []byte("\n" + delim)
	before, after, ok0 := bytes.Cut(rest, delimLine)
	if !ok0 {
		return nil, data
	}
	block := before
	tail := after
	// Consume the rest of the closing delimiter line (handles trailing CR/whitespace).
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	} else {
		tail = nil
	}
	var raw map[string]any
	if err := unmarshal(block, &raw); err != nil {
		return nil, data
	}
	return &Frontmatter{Format: format, Data: normalizeMap(raw)}, tail
}

func parseJSONFrontmatter(data []byte) (*Frontmatter, []byte) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, data
	}
	tail := data[dec.InputOffset():]
	// Strip a single leading newline so the body starts on the next line.
	tail = bytes.TrimPrefix(tail, []byte("\r\n"))
	tail = bytes.TrimPrefix(tail, []byte("\n"))
	return &Frontmatter{Format: "json", Data: normalizeMap(raw)}, tail
}

// normalizeMap recursively converts map[any]any to map[string]any so the
// result is cleanly addressable from CEL (which only supports string keys).
func normalizeMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return normalizeMap(x)
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, vv := range x {
			if ks, ok := k.(string); ok {
				m[ks] = normalizeValue(vv)
			}
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeValue(e)
		}
		return out
	}
	return v
}

// stringListValue coerces a front-matter value into a []string. A bare string
// is wrapped as a single-element list, matching how YAML/TOML users often
// write "tags: foo" instead of "tags: [foo]".
func stringListValue(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		if x == "" {
			return nil, true
		}
		return []string{x}, true
	}
	return nil, false
}

// timeValue coerces a front-matter value into a time.Time, accepting native
// time values (TOML) or several common string layouts (YAML, JSON).
func timeValue(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
			"2006/01/02",
		} {
			if t, err := time.Parse(layout, strings.TrimSpace(x)); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// stringValue extracts a string from a front-matter value, treating absence and
// empty as the same.
func stringValue(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}
