package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// buildSuperBlob synthesises a minimal SuperBlob containing one
// CodeDirectory and one entitlements blob. The exact layout matches
// real `codesign`-emitted signatures so the parser sees a realistic
// input shape — same bytes the real Mach-O LC_CODE_SIGNATURE points
// to. All multi-byte fields are big-endian per the spec.
type codeDirectorySpec struct {
	version    uint32
	flags      uint32
	hashType   uint8
	identifier string
	teamID     string
}

func buildCodeDirectory(spec codeDirectorySpec) []byte {
	// Reserve a header that's long enough for the v0x20200 layout
	// (through teamOffset). After the fixed header we append the
	// identifier + team ID as NUL-terminated strings.
	headerLen := uint32(52) // through teamOffset+4
	identBytes := append([]byte(spec.identifier), 0)
	teamBytes := append([]byte(spec.teamID), 0)
	identOffset := headerLen
	teamOffset := uint32(0)
	if spec.teamID != "" {
		teamOffset = identOffset + uint32(len(identBytes))
	}
	total := identOffset + uint32(len(identBytes))
	if spec.teamID != "" {
		total += uint32(len(teamBytes))
	}

	out := make([]byte, total)
	binary.BigEndian.PutUint32(out[0:4], csMagicCodeDirectory)
	binary.BigEndian.PutUint32(out[4:8], total)
	binary.BigEndian.PutUint32(out[8:12], spec.version)
	binary.BigEndian.PutUint32(out[12:16], spec.flags)
	binary.BigEndian.PutUint32(out[20:24], identOffset)
	out[37] = spec.hashType
	if spec.teamID != "" {
		binary.BigEndian.PutUint32(out[48:52], teamOffset)
	}

	copy(out[identOffset:], identBytes)
	if spec.teamID != "" {
		copy(out[teamOffset:], teamBytes)
	}
	return out
}

func buildEntitlementsBlob(xml string) []byte {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], csMagicEmbeddedEntitlements)
	binary.BigEndian.PutUint32(header[4:8], uint32(8+len(xml)))
	return append(header, []byte(xml)...)
}

func buildSuperBlob(blobs map[uint32][]byte) []byte {
	// Compute layout: 12-byte header + count*8-byte index + each blob.
	count := uint32(len(blobs))
	indexLen := 12 + int(count)*8
	totalLen := indexLen
	for _, b := range blobs {
		totalLen += len(b)
	}

	out := make([]byte, totalLen)
	binary.BigEndian.PutUint32(out[0:4], csMagicEmbeddedSignature)
	binary.BigEndian.PutUint32(out[4:8], uint32(totalLen))
	binary.BigEndian.PutUint32(out[8:12], count)

	// Stable slot ordering — sort keys for determinism. SuperBlobs in
	// the wild aren't strictly ordered but our tests want repeatable
	// layout.
	slots := make([]uint32, 0, count)
	for slot := range blobs {
		slots = append(slots, slot)
	}
	// Simple insertion sort to avoid an import.
	for i := 1; i < len(slots); i++ {
		for j := i; j > 0 && slots[j] < slots[j-1]; j-- {
			slots[j], slots[j-1] = slots[j-1], slots[j]
		}
	}

	pos := indexLen
	for i, slot := range slots {
		blob := blobs[slot]
		idxOff := 12 + i*8
		binary.BigEndian.PutUint32(out[idxOff:idxOff+4], slot)
		binary.BigEndian.PutUint32(out[idxOff+4:idxOff+8], uint32(pos))
		copy(out[pos:], blob)
		pos += len(blob)
	}
	return out
}

func TestParseCodeSignature_AppleSigned(t *testing.T) {
	cd := buildCodeDirectory(codeDirectorySpec{
		version:    cdVersionSupportsTeam,
		flags:      0, // Apple binaries don't typically set adhoc / hardened-runtime
		hashType:   csHashSHA256,
		identifier: "com.apple.libobjc-trampolines",
	})
	sb := buildSuperBlob(map[uint32][]byte{csSlotCodeDirectory: cd})

	info := parseCodeSignature(sb)

	if !info.Present {
		t.Fatal("expected Present=true")
	}
	if info.Identifier != "com.apple.libobjc-trampolines" {
		t.Errorf("identifier = %q", info.Identifier)
	}
	if info.TeamID != "" {
		t.Errorf("team_id = %q, want empty (Apple-signed)", info.TeamID)
	}
	if info.HashType != "sha256" {
		t.Errorf("hash_type = %q, want sha256", info.HashType)
	}
	if info.Adhoc {
		t.Error("Adhoc should be false")
	}
}

func TestParseCodeSignature_ThirdPartySigned(t *testing.T) {
	cd := buildCodeDirectory(codeDirectorySpec{
		version:    cdVersionSupportsTeam,
		flags:      cdFlagRuntime | cdFlagKill,
		hashType:   csHashSHA256,
		identifier: "com.example.app",
		teamID:     "ABC123DEF4",
	})
	sb := buildSuperBlob(map[uint32][]byte{csSlotCodeDirectory: cd})

	info := parseCodeSignature(sb)

	if info.TeamID != "ABC123DEF4" {
		t.Errorf("team_id = %q", info.TeamID)
	}
	if !info.HardenedRuntime {
		t.Error("HardenedRuntime should be true")
	}
	if !info.Killed {
		t.Error("Killed should be true")
	}
}

func TestParseCodeSignature_Adhoc(t *testing.T) {
	cd := buildCodeDirectory(codeDirectorySpec{
		version:    cdVersionSupportsTeam,
		flags:      cdFlagAdhoc,
		hashType:   csHashSHA256,
		identifier: "local-test",
	})
	sb := buildSuperBlob(map[uint32][]byte{csSlotCodeDirectory: cd})

	info := parseCodeSignature(sb)

	if !info.Adhoc {
		t.Error("Adhoc should be true")
	}
}

func TestParseCodeSignature_WithEntitlements(t *testing.T) {
	cd := buildCodeDirectory(codeDirectorySpec{
		version:    cdVersionSupportsTeam,
		flags:      cdFlagRuntime,
		hashType:   csHashSHA256,
		identifier: "com.example.network-app",
		teamID:     "TEAM123XYZ",
	})
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.app-sandbox</key>
	<true/>
	<key>com.apple.security.network.client</key>
	<true/>
	<key>com.apple.security.files.user-selected.read-write</key>
	<true/>
</dict>
</plist>`
	ent := buildEntitlementsBlob(xml)
	sb := buildSuperBlob(map[uint32][]byte{
		csSlotCodeDirectory: cd,
		csSlotEntitlements:  ent,
	})

	info := parseCodeSignature(sb)

	if !info.HasAppSandbox {
		t.Error("HasAppSandbox should be true")
	}
	if !info.HasNetworkClient {
		t.Error("HasNetworkClient should be true")
	}
	if !info.HasFullDiskAccess {
		t.Error("HasFullDiskAccess should be true (user-selected.read-write)")
	}
	if info.HasNetworkServer {
		t.Error("HasNetworkServer should be false")
	}
	wantKeys := []string{
		"com.apple.security.app-sandbox",
		"com.apple.security.files.user-selected.read-write",
		"com.apple.security.network.client",
	}
	if len(info.Entitlements) != len(wantKeys) {
		t.Fatalf("entitlements = %v, want %v", info.Entitlements, wantKeys)
	}
	for i, k := range wantKeys {
		if info.Entitlements[i] != k {
			t.Errorf("entitlements[%d] = %q, want %q", i, info.Entitlements[i], k)
		}
	}
}

func TestParseCodeSignature_MagicMismatch(t *testing.T) {
	junk := bytes.Repeat([]byte{0xFF}, 256)
	info := parseCodeSignature(junk)
	if info.Present {
		t.Error("Present should be false on magic mismatch")
	}
}

func TestParseCodeSignature_Truncated(t *testing.T) {
	// Magic only, no length.
	info := parseCodeSignature([]byte{0xfa, 0xde, 0x0c, 0xc0})
	if info.Present {
		t.Error("Present should be false on truncated input")
	}
}

func TestParseCodeSignature_BogusBlobOffsets(t *testing.T) {
	// SuperBlob header + index entry whose offset points past end-of-buffer.
	out := make([]byte, 20)
	binary.BigEndian.PutUint32(out[0:4], csMagicEmbeddedSignature)
	binary.BigEndian.PutUint32(out[4:8], 20)
	binary.BigEndian.PutUint32(out[8:12], 1) // count=1
	binary.BigEndian.PutUint32(out[12:16], csSlotCodeDirectory)
	binary.BigEndian.PutUint32(out[16:20], 9999) // out-of-bounds blob offset

	info := parseCodeSignature(out)
	// Should be Present=true (we read the SuperBlob header successfully)
	// but no CodeDirectory fields populated — the out-of-bounds offset
	// is silently skipped.
	if !info.Present {
		t.Error("Present should be true (SuperBlob header was valid)")
	}
	if info.Identifier != "" {
		t.Errorf("identifier should be empty, got %q", info.Identifier)
	}
}

func TestCodesignAttrs_Empty(t *testing.T) {
	attrs := codesignAttrs(codesignInfo{})
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for absent signature, got %v", attrs)
	}
}

func TestCodesignAttrs_AppleSignedHeuristic(t *testing.T) {
	attrs := codesignAttrs(codesignInfo{
		Present:    true,
		Identifier: "com.apple.thing",
		HashType:   "sha256",
	})
	if attrs["is_apple_signed"] != true {
		t.Errorf("is_apple_signed = %v, want true (empty team_id + not adhoc)", attrs["is_apple_signed"])
	}
	if _, ok := attrs["is_third_party_signed"]; ok {
		t.Error("is_third_party_signed should be absent for Apple signature")
	}
}

func TestCodesignAttrs_ThirdPartySignedDoesNotImplyApple(t *testing.T) {
	attrs := codesignAttrs(codesignInfo{
		Present: true,
		TeamID:  "Q6L2SF6YDW",
	})
	if _, ok := attrs["is_apple_signed"]; ok {
		t.Error("is_apple_signed should be absent when team_id is non-empty")
	}
	if attrs["is_third_party_signed"] != true {
		t.Error("is_third_party_signed should be true")
	}
}

func TestCodesignAttrs_AdhocIsNotAppleSigned(t *testing.T) {
	attrs := codesignAttrs(codesignInfo{
		Present: true,
		Adhoc:   true,
	})
	if _, ok := attrs["is_apple_signed"]; ok {
		t.Error("is_apple_signed should be absent for adhoc-signed (no team_id but explicitly adhoc)")
	}
}

// TestReadMachoInfo_RealAppleSignedDylib pulls a known system .dylib
// off disk and asserts the full pipeline lights up correctly. Skipped
// on non-darwin since the system path doesn't exist.
func TestReadMachoInfo_RealAppleSignedDylib(t *testing.T) {
	const path = "/usr/lib/libobjc-trampolines.dylib"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("skipping; %s not present", path)
	}
	dir, base := filepath.Split(path)
	fsys := os.DirFS(dir)

	attrs, err := readMachoInfo(context.Background(), fsys, base)
	if err != nil {
		t.Fatal(err)
	}
	if attrs["is_codesigned"] != true {
		t.Error("is_codesigned should be true on Apple-signed dylib")
	}
	if attrs["is_apple_signed"] != true {
		t.Error("is_apple_signed should be true (empty team_id)")
	}
	if got := attrs["codesign_identifier"]; got != "com.apple.libobjc-trampolines" {
		t.Errorf("codesign_identifier = %q", got)
	}
}

// Sanity check that the section-reader plumbing for fat arch slices
// doesn't drop the signature data. Uses an io.SectionReader over an
// in-memory buffer.
func TestSectionReaderAt_RoundTrip(t *testing.T) {
	buf := bytes.NewReader([]byte("hello world"))
	sr := sectionReaderAt(buf, 6, 5)
	out := make([]byte, 5)
	if _, err := sr.ReadAt(out, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if string(out) != "world" {
		t.Errorf("got %q, want world", out)
	}
}
