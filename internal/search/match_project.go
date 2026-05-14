package search

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// alwaysOnFields are kept on every Match regardless of the projection
// whitelist. path identifies which file the match is about — projecting
// it away would defeat the purpose. content_type and size are tiny
// scalars that every consumer wants at least for sanity; keeping them
// always-on means the existing default wire shape doesn't change when
// no projection is requested AND existing tag-omitempty semantics stay
// intact.
var alwaysOnFields = map[string]struct{}{
	"path":         {},
	"content_type": {},
	"size":         {},
}

// matchFieldsOnce memoises the json-tag → struct-field-index map built
// via reflection on Match. Reflection is cheap but not free; this is
// hit on every projected response.
var (
	matchFieldsOnce sync.Once
	matchFieldsIdx  map[string]int // json-tag-name → struct-field index
	matchFieldNames map[string]struct{}
)

func matchFields() (map[string]int, map[string]struct{}) {
	matchFieldsOnce.Do(func() {
		t := reflect.TypeFor[Match]()
		matchFieldsIdx = make(map[string]int, t.NumField())
		matchFieldNames = make(map[string]struct{}, t.NumField())
		for i := range t.NumField() {
			tag := t.Field(i).Tag.Get("json")
			name := jsonTagName(tag)
			if name == "" || name == "-" {
				continue
			}
			matchFieldsIdx[name] = i
			matchFieldNames[name] = struct{}{}
		}
	})
	return matchFieldsIdx, matchFieldNames
}

// jsonTagName returns the name portion of a `json:"..."` tag —
// everything before the first comma. Empty for "-" / unset tags.
func jsonTagName(tag string) string {
	if before, _, ok := strings.Cut(tag, ","); ok {
		return before
	}
	return tag
}

// MatchFieldNames returns the canonical set of json-tag names declared
// on Match — the source of truth for what `fields` may contain. Useful
// for callers that want to build "valid keys" lists for error messages.
// Returned slice is sorted for deterministic output.
func MatchFieldNames() []string {
	_, names := matchFields()
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ValidateFields checks that every entry in fields is a recognised
// Match json-tag name. Returns a clear error naming the first unknown
// entry — agents should fix the request rather than receive partial
// results with silently-dropped projections. Empty fields slice is
// allowed and returns nil.
func ValidateFields(fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	_, known := matchFields()
	for _, f := range fields {
		if _, ok := known[f]; !ok {
			return fmt.Errorf("unknown field %q (call list_attributes for the canonical schema, or omit 'fields' to get every populated attribute)", f)
		}
	}
	return nil
}

// ProjectMatch returns m with every field whose json-tag name is NOT
// in the allowlist zeroed out. path / content_type / size are kept
// regardless (see alwaysOnFields). When allow is empty / nil, the
// input is returned unchanged.
//
// The zero-value strategy relies on the Match struct's `omitempty`
// json tags: a zeroed field with omitempty serialises to nothing. The
// three always-on fields don't carry omitempty, so they still appear
// in the JSON payload even when zero.
func ProjectMatch(m Match, allow []string) Match {
	if len(allow) == 0 {
		return m
	}
	allowSet := make(map[string]struct{}, len(allow))
	for _, f := range allow {
		allowSet[f] = struct{}{}
	}
	idx, _ := matchFields()
	// Operate on a fresh addressable copy so we can SetZero individual
	// fields. Reflect on a value-copy of m via &m → Elem so the caller's
	// slice element isn't mutated.
	v := reflect.ValueOf(&m).Elem()
	for name, fi := range idx {
		if _, on := alwaysOnFields[name]; on {
			continue
		}
		if _, on := allowSet[name]; on {
			continue
		}
		v.Field(fi).SetZero()
	}
	return m
}

// ProjectMatches applies ProjectMatch to every entry in ms in place.
// Convenience wrapper for the MCP handlers' []Match → []Match path.
func ProjectMatches(ms []Match, allow []string) {
	if len(allow) == 0 {
		return
	}
	for i := range ms {
		ms[i] = ProjectMatch(ms[i], allow)
	}
}
