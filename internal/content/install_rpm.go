package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
	"strings"
)

// rpmLeadSize is the fixed size of the legacy RPM Lead structure.
// The Lead has been deprecated since RPM 4.x — real metadata now
// lives in the Header that follows — but it's still always present
// and carries enough for v1 triage (name + arch + binary-vs-source).
const rpmLeadSize = 96

// rpmMagic is the 4-byte RPM Lead signature.
var rpmMagic = []byte{0xED, 0xAB, 0xEE, 0xDB}

// rpmArchNames maps the Lead's archnum field to canonical lowercase
// architecture strings. The full table from rpm-org/rpm rpmrc.in is
// hundreds of entries (one per OS × arch combo); these are the
// architectures still in active use. Unknown values surface as
// "unknown-N" so agents can grep them.
var rpmArchNames = map[uint16]string{
	1:  "i386",
	2:  "alpha",
	3:  "sparc",
	4:  "mips",
	5:  "ppc",
	6:  "m68k",
	7:  "sgi",
	8:  "rs6000",
	9:  "ia64",
	10: "sparc64",
	11: "mipsel",
	12: "arm",
	13: "m68kmint",
	14: "s390",
	15: "s390x",
	16: "ppc64",
	17: "sh",
	18: "xtensa",
	19: "x86_64",
	20: "ppc64le",
	21: "aarch64",
	22: "mips64",
	23: "mips64el",
	24: "riscv64",
	25: "loongarch64",
}

// readRPMInfo parses the RPM Lead at offset 0 of an RPM package.
//
// Lead layout (all big-endian; ASCII text fields are NUL-padded):
//
//	0x00 [4]    Magic 0xEDABEEDB
//	0x04 [1]    Major version (currently 3 or 4)
//	0x05 [1]    Minor version
//	0x06 [2]    Type    (0=binary, 1=source)
//	0x08 [2]    Archnum
//	0x0A [66]   Name in the format <name>-<version>-<release>
//	0x4C [2]    Osnum   (1=Linux)
//	0x4E [2]    Signature type
//	0x50 [16]   Reserved
//
// Surfaces:
//   - package_name    — everything before the final two "-" segments
//   - package_version — second-to-last "-" segment
//   - package_release — last "-" segment (often distro-specific build id)
//   - package_arch    — canonical lowercase from rpmArchNames
//   - package_kind    — "binary" or "source"
func readRPMInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var lead [rpmLeadSize]byte
	if _, err := io.ReadFull(f, lead[:]); err != nil {
		return Attributes{}, nil
	}
	if !bytes.Equal(lead[0:4], rpmMagic) {
		return Attributes{}, nil
	}
	return parseRPMLead(lead[:]), nil
}

func parseRPMLead(lead []byte) Attributes {
	if len(lead) < rpmLeadSize || !bytes.Equal(lead[0:4], rpmMagic) {
		return Attributes{}
	}
	typeNum := binary.BigEndian.Uint16(lead[0x06 : 0x06+2])
	archNum := binary.BigEndian.Uint16(lead[0x08 : 0x08+2])

	// Name field is NUL-terminated within its 66 bytes.
	nameBytes := lead[0x0A : 0x0A+66]
	if i := bytes.IndexByte(nameBytes, 0); i >= 0 {
		nameBytes = nameBytes[:i]
	}
	fullName := strings.TrimSpace(string(nameBytes))

	extras := Attributes{}
	switch typeNum {
	case 0:
		extras["package_kind"] = "binary"
	case 1:
		extras["package_kind"] = "source"
	}

	if arch, ok := rpmArchNames[archNum]; ok {
		extras["package_arch"] = arch
	} else if archNum != 0 {
		extras["package_arch"] = "unknown"
	}

	if fullName != "" {
		name, version, release := splitRPMName(fullName)
		if name != "" {
			extras["package_name"] = name
		}
		if version != "" {
			extras["package_version"] = version
		}
		if release != "" {
			extras["package_release"] = release
		}
	}
	return installPackageAttrs("rpm", extras)
}

// splitRPMName decomposes an RPM Lead name like
// "openssh-clients-8.7p1-38.el9_3.4" into (name, version, release).
// The format is convention, not contract — a name can contain dashes
// (and the version can too). The standard parse is "split on the last
// two dashes from the right": everything before is the package name,
// the middle chunk is the version, the last is the release.
//
// Names with fewer than two dashes return (name, "", "") — better to
// surface a partial parse than to lose the name entirely.
func splitRPMName(s string) (name, version, release string) {
	lastDash := strings.LastIndexByte(s, '-')
	if lastDash < 0 {
		return s, "", ""
	}
	release = s[lastDash+1:]
	rest := s[:lastDash]
	prevDash := strings.LastIndexByte(rest, '-')
	if prevDash < 0 {
		return rest, "", release
	}
	version = rest[prevDash+1:]
	name = rest[:prevDash]
	return name, version, release
}
