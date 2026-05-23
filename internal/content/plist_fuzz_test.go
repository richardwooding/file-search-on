package content

import (
	"bytes"
	"strings"
	"testing"

	"howett.net/plist"
)

// FuzzParsePlist targets parsePlist with adversarial inputs. The
// howett.net/plist parser is the trust boundary — most defects would
// surface as panics on malformed bplist00 trailer offsets or oversized
// XML reads. Our wrapper catches those via the normal `if err != nil`
// branch; the fuzz body asserts no panic and consistent attribute
// shape (when populated, types match the schema).
func FuzzParsePlist(f *testing.F) {
	// Seed 1: minimal valid binary plist (Info.plist-shaped dict).
	var bin1 bytes.Buffer
	_ = plist.NewEncoderForFormat(&bin1, plist.BinaryFormat).Encode(map[string]any{
		"CFBundleIdentifier": "com.example.app",
		"CFBundleVersion":    "1.0",
	})
	f.Add(bin1.Bytes(), "/Applications/Foo.app/Contents/Info.plist")

	// Seed 2: minimal XML plist.
	var xml1 bytes.Buffer
	_ = plist.NewEncoderForFormat(&xml1, plist.XMLFormat).Encode(map[string]any{
		"k": "v",
	})
	f.Add(xml1.Bytes(), "/tmp/x.plist")

	// Seed 3: LaunchAgent-shaped dict with ProgramArguments array.
	var bin2 bytes.Buffer
	_ = plist.NewEncoderForFormat(&bin2, plist.BinaryFormat).Encode(map[string]any{
		"Label":            "com.example.agent",
		"ProgramArguments": []any{"/bin/sh", "-c", "echo hi"},
		"RunAtLoad":        true,
	})
	f.Add(bin2.Bytes(), "/Library/LaunchAgents/com.example.agent.plist")

	// Seed 4: bplist00 magic only, nothing else — must NOT panic on
	// the trailer offset read.
	f.Add([]byte("bplist00"), "/tmp/short.plist")

	// Seed 5: all-0xFF noise — wrong format, must degrade silently.
	junk := make([]byte, 256)
	for i := range junk {
		junk[i] = 0xFF
	}
	f.Add(junk, "/tmp/junk.plist")

	// Seed 6: bplist00 + arbitrary truncated bytes. The howett parser
	// reads a 32-byte trailer at EOF — short inputs that pass the
	// magic check are the highest-value adversarial shape.
	f.Add([]byte("bplist00\x01\x02\x03"), "/tmp/trunc.plist")

	// Seed 7: empty input.
	f.Add([]byte{}, "")

	// Seed 8: XML with deeply nested arrays — parser shouldn't blow
	// the stack on reasonable depths.
	var deep strings.Builder
	deep.WriteString(`<?xml version="1.0"?><plist><array>`)
	for range 100 {
		deep.WriteString(`<array>`)
	}
	for range 100 {
		deep.WriteString(`</array>`)
	}
	deep.WriteString(`</array></plist>`)
	f.Add([]byte(deep.String()), "/tmp/deep.plist")

	f.Fuzz(func(t *testing.T, data []byte, path string) {
		attrs := parsePlist(data, path)
		// Shape contract: every populated attribute matches its declared
		// type. If parsePlist sneaks in a wrong type, the activation map
		// would silently surface the zero value, which is a real bug.
		if v, ok := attrs["plist_format"]; ok {
			if _, ok := v.(string); !ok {
				t.Fatalf("plist_format wrong type: %T", v)
			}
		}
		if v, ok := attrs["plist_program_arguments"]; ok {
			if _, ok := v.([]string); !ok {
				t.Fatalf("plist_program_arguments wrong type: %T", v)
			}
		}
		if v, ok := attrs["plist_run_at_load"]; ok {
			if _, ok := v.(bool); !ok {
				t.Fatalf("plist_run_at_load wrong type: %T", v)
			}
		}
		if v, ok := attrs["plist_keep_alive"]; ok {
			if _, ok := v.(bool); !ok {
				t.Fatalf("plist_keep_alive wrong type: %T", v)
			}
		}
	})
}
