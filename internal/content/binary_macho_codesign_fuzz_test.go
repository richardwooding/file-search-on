package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// FuzzParseCodeSignature exercises the SuperBlob + CodeDirectory
// binary parser with adversarial inputs. The SuperBlob format is a
// counted-index of typed sub-blobs with absolute offsets — exactly
// the territory where bounds-check bugs hide. The parser's hard
// contract: no panic, regardless of input.
func FuzzParseCodeSignature(f *testing.F) {
	// Seed 1: well-formed minimal SuperBlob with one CodeDirectory.
	cd := buildCodeDirectory(codeDirectorySpec{
		version:    cdVersionSupportsTeam,
		flags:      cdFlagRuntime,
		hashType:   csHashSHA256,
		identifier: "com.example.app",
		teamID:     "ABC123DEF4",
	})
	f.Add(buildSuperBlob(map[uint32][]byte{csSlotCodeDirectory: cd}))

	// Seed 2: SuperBlob with both CodeDirectory and entitlements.
	xml := `<?xml version="1.0"?><plist version="1.0"><dict><key>com.apple.security.app-sandbox</key><true/></dict></plist>`
	ent := buildEntitlementsBlob(xml)
	f.Add(buildSuperBlob(map[uint32][]byte{
		csSlotCodeDirectory: cd,
		csSlotEntitlements:  ent,
	}))

	// Seed 3: magic only — must NOT panic when length / count fields
	// are truncated.
	f.Add([]byte{0xfa, 0xde, 0x0c, 0xc0})

	// Seed 4: empty input.
	f.Add([]byte{})

	// Seed 5: all-0xFF junk — wrong magic, must degrade silently.
	junk := bytes.Repeat([]byte{0xFF}, 256)
	f.Add(junk)

	// Seed 6: valid magic + claimed length larger than the buffer.
	overclaim := make([]byte, 12)
	binary.BigEndian.PutUint32(overclaim[0:4], csMagicEmbeddedSignature)
	binary.BigEndian.PutUint32(overclaim[4:8], 1<<24) // claimed 16 MiB
	binary.BigEndian.PutUint32(overclaim[8:12], 1)
	f.Add(overclaim)

	// Seed 7: SuperBlob claiming a million sub-blobs (out of band).
	huge := make([]byte, 16)
	binary.BigEndian.PutUint32(huge[0:4], csMagicEmbeddedSignature)
	binary.BigEndian.PutUint32(huge[4:8], 16)
	binary.BigEndian.PutUint32(huge[8:12], 1_000_000)
	f.Add(huge)

	// Seed 8: CodeDirectory with version requesting team_offset past
	// end-of-blob.
	bogus := make([]byte, 52)
	binary.BigEndian.PutUint32(bogus[0:4], csMagicCodeDirectory)
	binary.BigEndian.PutUint32(bogus[4:8], 52)
	binary.BigEndian.PutUint32(bogus[8:12], cdVersionSupportsTeam)
	binary.BigEndian.PutUint32(bogus[20:24], 60)  // identOffset past EOB
	binary.BigEndian.PutUint32(bogus[48:52], 999) // teamOffset past EOB
	f.Add(buildSuperBlob(map[uint32][]byte{csSlotCodeDirectory: bogus}))

	f.Fuzz(func(t *testing.T, data []byte) {
		info := parseCodeSignature(data)
		// Shape contract: when Present is true, the fields we read are
		// either zero-valued or sane strings. Negative integer values
		// would mean we mis-decoded a big-endian field; the parser
		// uses uint32 throughout so this is mainly a sanity check on
		// downstream codesignAttrs.
		if info.Present {
			attrs := codesignAttrs(info)
			if got, ok := attrs["is_codesigned"]; ok {
				if _, ok := got.(bool); !ok {
					t.Fatalf("is_codesigned wrong type: %T", got)
				}
			}
			if got, ok := attrs["entitlements"]; ok {
				if _, ok := got.([]string); !ok {
					t.Fatalf("entitlements wrong type: %T", got)
				}
			}
		}
	})
}
