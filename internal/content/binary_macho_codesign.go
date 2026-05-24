package content

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"io"
	"sort"
	"strings"

	"howett.net/plist"
)

// Mach-O code-signature parsing per
// https://github.com/apple-oss-distributions/Security
// (OSX/libsecurity_codesigning/lib/codedirectory.h).
//
// The LC_CODE_SIGNATURE load command points to a SuperBlob in the file
// — a typed container holding the CodeDirectory (identifier, team ID,
// flags, hash type) plus the entitlements blob (an XML plist) plus the
// CMS cert chain (BlobWrapper — not parsed in v1; v1 leaves
// `codesign_authority` empty and falls back to a team-ID-based
// is_apple_signed heuristic).
//
// All multi-byte fields in the signature blobs are big-endian on disk
// regardless of the host or Mach-O byte order — the spec is explicit.

const (
	// Load command IDs.
	lcCodeSignature uint32 = 0x1d

	// Blob magics.
	csMagicEmbeddedSignature       uint32 = 0xfade0cc0
	csMagicCodeDirectory           uint32 = 0xfade0c02
	csMagicEmbeddedEntitlements    uint32 = 0xfade7171
	csMagicEmbeddedDerEntitlements uint32 = 0xfade7172
	csMagicRequirementSet          uint32 = 0xfade0c01
	csMagicBlobWrapper             uint32 = 0xfade0b01

	// SuperBlob slot types we care about.
	csSlotCodeDirectory   uint32 = 0
	csSlotEntitlements    uint32 = 5
	csSlotDerEntitlements uint32 = 7

	// CodeDirectory header offsets (big-endian).
	cdMagicOffset       = 0
	cdLengthOffset      = 4
	cdVersionOffset     = 8
	cdFlagsOffset       = 12
	cdHashOffsetOffset  = 16
	cdIdentOffsetOffset = 20
	cdNSpecialSlots     = 24
	cdNCodeSlots        = 28
	cdCodeLimit         = 32
	cdHashSize          = 36
	cdHashType          = 37
	cdPlatform          = 38
	cdPageSize          = 39
	cdSpare2            = 40
	// version 0x20200+
	cdTeamOffset = 48

	// CodeDirectory flags.
	cdFlagAdhoc             uint32 = 0x00000002
	cdFlagKill              uint32 = 0x00000200
	cdFlagLibraryValidation uint32 = 0x00002000
	cdFlagRuntime           uint32 = 0x00010000

	// CodeDirectory version that introduces the team_offset field.
	cdVersionSupportsTeam uint32 = 0x20200

	// Hash type IDs.
	csHashSHA1            uint8 = 1
	csHashSHA256          uint8 = 2
	csHashSHA256Truncated uint8 = 3
	csHashSHA384          uint8 = 4
	csHashSHA512          uint8 = 5

	// Defensive caps to keep adversarial input bounded. A real
	// SuperBlob is at most a few KB; the cert chain blob is the
	// largest part and rarely exceeds ~30 KB. 1 MiB is generous.
	csMaxSuperBlobLen = 1 << 20
	// Entitlement keys are short property-list keys; thousands of
	// them is already pathological.
	csMaxEntitlementKeys = 1024
)

// codesignInfo holds the parsed code-signature surface. Empty fields
// mean "not present in this signature" — the parser doesn't synthesize
// defaults. The codesignAttrs function maps this struct into the
// Attributes map that the celexpr layer consumes.
type codesignInfo struct {
	Identifier        string
	TeamID            string
	HashType          string
	Flags             uint32
	HardenedRuntime   bool
	LibraryValidation bool
	Killed            bool
	Adhoc             bool
	Entitlements      []string
	HasAppSandbox     bool
	HasFullDiskAccess bool
	HasNetworkClient  bool
	HasNetworkServer  bool
	// Present is set when we successfully parsed at least the
	// SuperBlob header — distinguishes "no signature at all" from
	// "signature exists but is empty / malformed".
	Present bool
}

// parseCodeSignature is the pure-function entry point exercised by
// tests + fuzz. Walks the SuperBlob, decodes the CodeDirectory, and
// extracts entitlement keys. Returns an empty info on magic mismatch
// or short input — never panics.
func parseCodeSignature(data []byte) codesignInfo {
	if len(data) < 12 {
		return codesignInfo{}
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	// bytes 4..8 carry the SuperBlob's claimed length, but the
	// LINKEDIT segment often pads past the signature so we honour
	// what we have via len(data) bounds checks instead of trusting
	// the length field.
	count := binary.BigEndian.Uint32(data[8:12])

	if magic != csMagicEmbeddedSignature {
		return codesignInfo{}
	}
	if count > 64 {
		// Real SuperBlobs carry 1-5 slots — anything higher is
		// either malformed or adversarial.
		return codesignInfo{Present: true}
	}

	info := codesignInfo{Present: true}

	// SuperBlob index: count entries of (type uint32, offset uint32).
	indexStart := 12
	indexEnd := indexStart + int(count)*8
	if indexEnd > len(data) {
		return info
	}

	for i := range int(count) {
		entryOff := indexStart + i*8
		slotType := binary.BigEndian.Uint32(data[entryOff : entryOff+4])
		blobOff := binary.BigEndian.Uint32(data[entryOff+4 : entryOff+8])

		if int(blobOff) >= len(data) || int(blobOff)+8 > len(data) {
			continue
		}
		blobMagic := binary.BigEndian.Uint32(data[blobOff : blobOff+4])
		blobLen := binary.BigEndian.Uint32(data[blobOff+4 : blobOff+8])

		if int(blobOff)+int(blobLen) > len(data) || blobLen < 8 {
			continue
		}
		blob := data[blobOff : blobOff+blobLen]

		switch {
		case slotType == csSlotCodeDirectory && blobMagic == csMagicCodeDirectory:
			parseCodeDirectory(blob, &info)
		case slotType == csSlotEntitlements && blobMagic == csMagicEmbeddedEntitlements:
			parseEntitlementsXML(blob, &info)
		}
	}

	// Alternate CodeDirectories — slots 0x1000..0x1004 — may carry a
	// modern hash variant (e.g. SHA-256) when the primary slot 0 is
	// the legacy SHA-1 for compatibility. We don't merge them in v1;
	// the primary CodeDirectory's fields are the canonical surface.

	return info
}

// parseCodeDirectory decodes the CodeDirectory blob's identifier,
// team ID, hash type, and flags into the codesignInfo. Tolerant of
// versioned field offsets — fields beyond the version threshold are
// skipped on older signatures.
func parseCodeDirectory(blob []byte, info *codesignInfo) {
	if len(blob) < cdSpare2+4 {
		return
	}
	version := binary.BigEndian.Uint32(blob[cdVersionOffset : cdVersionOffset+4])
	flags := binary.BigEndian.Uint32(blob[cdFlagsOffset : cdFlagsOffset+4])
	identOffset := binary.BigEndian.Uint32(blob[cdIdentOffsetOffset : cdIdentOffsetOffset+4])
	hashType := blob[cdHashType]

	info.Flags = flags
	info.Adhoc = flags&cdFlagAdhoc != 0
	info.Killed = flags&cdFlagKill != 0
	info.LibraryValidation = flags&cdFlagLibraryValidation != 0
	info.HardenedRuntime = flags&cdFlagRuntime != 0
	info.HashType = hashTypeName(hashType)

	if identOffset > 0 && int(identOffset) < len(blob) {
		info.Identifier = readCString(blob[identOffset:])
	}

	// Team ID landed in CodeDirectory version 0x20200. Older
	// signatures (FreeBSD-style, pre-Sierra adhoc, …) don't have it
	// and we leave the field empty.
	if version >= cdVersionSupportsTeam && len(blob) >= cdTeamOffset+4 {
		teamOffset := binary.BigEndian.Uint32(blob[cdTeamOffset : cdTeamOffset+4])
		if teamOffset > 0 && int(teamOffset) < len(blob) {
			info.TeamID = readCString(blob[teamOffset:])
		}
	}
}

// parseEntitlementsXML decodes the embedded entitlements plist (an
// 8-byte blob header + XML plist payload) into the canonical
// predicates plus a sorted list of all keys. Uses the existing
// howett.net/plist parser — no separate XML walker.
func parseEntitlementsXML(blob []byte, info *codesignInfo) {
	if len(blob) <= 8 {
		return
	}
	// Strip the 8-byte blob header (magic + length).
	payload := blob[8:]

	var root any
	if _, err := plist.Unmarshal(payload, &root); err != nil {
		return
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
		if len(keys) >= csMaxEntitlementKeys {
			break
		}
	}
	sort.Strings(keys)
	info.Entitlements = keys

	// Canonical predicates. Map known entitlement keys to is_X bool
	// surfaces so agents don't have to remember the exact key strings.
	if b, ok := dict["com.apple.security.app-sandbox"].(bool); ok && b {
		info.HasAppSandbox = true
	}
	if b, ok := dict["com.apple.security.network.client"].(bool); ok && b {
		info.HasNetworkClient = true
	}
	if b, ok := dict["com.apple.security.network.server"].(bool); ok && b {
		info.HasNetworkServer = true
	}
	// Full-disk-access on macOS isn't a single entitlement key —
	// it's any of these grants. Match the umbrella behaviour of
	// `tccutil` / System Settings → Full Disk Access.
	for _, k := range []string{
		"com.apple.security.files.all",
		"com.apple.security.files.user-selected.read-write",
		"com.apple.private.tcc.allow",
	} {
		if v, ok := dict[k]; ok {
			switch x := v.(type) {
			case bool:
				if x {
					info.HasFullDiskAccess = true
				}
			case []any:
				if len(x) > 0 {
					info.HasFullDiskAccess = true
				}
			}
		}
	}
}

// hashTypeName maps the CodeDirectory hashType byte to the canonical
// short name. Returns "" for unknown values rather than guessing.
func hashTypeName(t uint8) string {
	switch t {
	case csHashSHA1:
		return "sha1"
	case csHashSHA256:
		return "sha256"
	case csHashSHA256Truncated:
		return "sha256-truncated"
	case csHashSHA384:
		return "sha384"
	case csHashSHA512:
		return "sha512"
	}
	return ""
}

// readCString reads a NUL-terminated string from the start of buf.
// Returns the prefix up to (but not including) the first NUL, or the
// full buffer trimmed if no NUL is present.
func readCString(buf []byte) string {
	if before, _, ok := bytes.Cut(buf, []byte{0}); ok {
		return strings.TrimSpace(string(before))
	}
	return strings.TrimSpace(string(buf))
}

// readCodeSignatureBytes locates the LC_CODE_SIGNATURE load command
// in the Mach-O file, then reads the SuperBlob bytes at the indicated
// file offset via the underlying io.ReaderAt. Returns nil + nil error
// when the file isn't code-signed (the common case for `.o` object
// files, dev-builds, etc.).
func readCodeSignatureBytes(f *macho.File, ra io.ReaderAt) ([]byte, error) {
	for _, load := range f.Loads {
		raw, ok := load.(macho.LoadBytes)
		if !ok || len(raw) < 16 {
			continue
		}
		cmd := f.ByteOrder.Uint32(raw[0:4])
		if cmd != lcCodeSignature {
			continue
		}
		dataOff := f.ByteOrder.Uint32(raw[8:12])
		dataSize := f.ByteOrder.Uint32(raw[12:16])
		if dataSize == 0 || dataSize > csMaxSuperBlobLen {
			return nil, nil
		}
		buf := make([]byte, dataSize)
		if _, err := ra.ReadAt(buf, int64(dataOff)); err != nil {
			return nil, err
		}
		return buf, nil
	}
	return nil, nil
}

// codesignAttrs maps the parsed info into the Attributes the celexpr
// layer consumes. Empty / zero-valued fields are omitted so the JSON
// wire shape stays clean for unsigned binaries.
func codesignAttrs(info codesignInfo) Attributes {
	out := Attributes{}
	if !info.Present {
		return out
	}
	out["is_codesigned"] = true
	if info.Identifier != "" {
		out["codesign_identifier"] = info.Identifier
	}
	if info.TeamID != "" {
		out["codesign_team_id"] = info.TeamID
	}
	if info.HashType != "" {
		out["codesign_hash_type"] = info.HashType
	}
	if info.HardenedRuntime {
		out["codesign_hardened_runtime"] = true
	}
	if info.LibraryValidation {
		out["codesign_library_validation"] = true
	}
	if info.Killed {
		out["codesign_killed"] = true
	}
	if info.Adhoc {
		out["codesign_adhoc"] = true
	}
	if len(info.Entitlements) > 0 {
		out["entitlements"] = info.Entitlements
	}
	if info.HasAppSandbox {
		out["entitlement_app_sandbox"] = true
	}
	if info.HasFullDiskAccess {
		out["entitlement_full_disk_access"] = true
	}
	if info.HasNetworkClient {
		out["entitlement_network_client"] = true
	}
	if info.HasNetworkServer {
		out["entitlement_network_server"] = true
	}
	// Apple-signed heuristic per the issue's v1 plan: empty team_id +
	// not adhoc + is_codesigned. Apple's first-party binaries don't
	// carry a Team ID (those are reserved for Apple Developer
	// accounts); adhoc-signed binaries set the adhoc flag explicitly.
	// Full cert-chain parsing is a follow-up; this heuristic catches
	// the dominant case correctly on a real /Applications walk.
	if info.TeamID == "" && !info.Adhoc {
		out["is_apple_signed"] = true
	}
	if info.TeamID != "" {
		out["is_third_party_signed"] = true
	}
	return out
}
