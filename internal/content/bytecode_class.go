package content

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
)

// JVM .class file constants.
const classMagic uint32 = 0xCAFEBABE

const (
	classMaxPoolEntries  = 65535 // JVMS §4.4: u2 constant_pool_count
	classMaxInterfaces   = 65535
	classMaxFields       = 65535
	classMaxMethods      = 65535
	classMaxStringBytes  = 65535
)

// JVM constant-pool tag IDs (JVMS §4.4 table). We only care about
// CONSTANT_Utf8 (the actual name strings) and CONSTANT_Class (which
// indirects through a name_index to a CONSTANT_Utf8). Other tags
// are walked just enough to advance the cursor.
const (
	cpTagUtf8               = 1
	cpTagInteger            = 3
	cpTagFloat              = 4
	cpTagLong               = 5
	cpTagDouble             = 6
	cpTagClass              = 7
	cpTagString             = 8
	cpTagFieldref           = 9
	cpTagMethodref          = 10
	cpTagInterfaceMethodref = 11
	cpTagNameAndType        = 12
	cpTagMethodHandle       = 15
	cpTagMethodType         = 16
	cpTagDynamic            = 17
	cpTagInvokeDynamic      = 18
	cpTagModule             = 19
	cpTagPackage            = 20
)

// JVM class-level access flags (JVMS §4.1 table).
const (
	accPublic     uint16 = 0x0001
	accFinal      uint16 = 0x0010
	accSuper      uint16 = 0x0020
	accInterface  uint16 = 0x0200
	accAbstract   uint16 = 0x0400
	accSynthetic  uint16 = 0x1000
	accAnnotation uint16 = 0x2000
	accEnum       uint16 = 0x4000
	accModule     uint16 = 0x8000
)

// classReadCap is a sanity ceiling on bytes read from a .class file.
// Real .class files are well under 4 MiB even for synthetic monsters.
// 16 MiB is generous; above that the file still detects but
// attribute extraction stops.
const classReadCap = 16 * 1024 * 1024

// readClassInfo parses a JVM .class file header per JVMS §4.1.
// Surfaces class_name, super_class, interfaces (list), method_count,
// field_count, access_flags (list of canonical strings) plus the
// cross-format bytecode_format = "jvm" + runtime_version (e.g.
// "Java 17"). Fields / methods are counted, not walked — their
// attribute payloads aren't surfaced.
func readClassInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, classReadCap))
	if err != nil {
		return Attributes{}, nil
	}
	return parseClassFile(buf), nil
}

func parseClassFile(data []byte) Attributes {
	r := &classReader{data: data}
	if !r.checkMagic() {
		return Attributes{}
	}
	if _, ok := r.u2(); !ok { // minor
		return Attributes{}
	}
	major, ok := r.u2()
	if !ok {
		return Attributes{}
	}
	// Past this point we have at least the format version; emit
	// best-effort attrs on any failure rather than dropping.
	cpStrings, cpClasses, ok := r.constantPool()
	if !ok {
		return bytecodeAttrs("jvm", javaVersion(major), nil)
	}
	accessFlags, ok := r.u2()
	if !ok {
		return bytecodeAttrs("jvm", javaVersion(major), nil)
	}
	thisClass, _ := r.u2()
	superClass, _ := r.u2()
	ifaceCount, ok := r.u2()
	if !ok || int(ifaceCount) > classMaxInterfaces {
		return assembleClassAttrs(major, accessFlags, thisClass, superClass, cpClasses, cpStrings, nil, 0, 0)
	}
	interfaces := make([]string, 0, ifaceCount)
	for range int(ifaceCount) {
		idx, ok := r.u2()
		if !ok {
			break
		}
		if name := resolveClassName(idx, cpClasses, cpStrings); name != "" {
			interfaces = append(interfaces, name)
		}
	}
	fieldCount, ok := r.u2()
	if !ok || int(fieldCount) > classMaxFields {
		return assembleClassAttrs(major, accessFlags, thisClass, superClass, cpClasses, cpStrings, interfaces, 0, 0)
	}
	if !r.skipMembers(int(fieldCount)) {
		return assembleClassAttrs(major, accessFlags, thisClass, superClass, cpClasses, cpStrings, interfaces, int64(fieldCount), 0)
	}
	methodCount, ok := r.u2()
	if !ok || int(methodCount) > classMaxMethods {
		return assembleClassAttrs(major, accessFlags, thisClass, superClass, cpClasses, cpStrings, interfaces, int64(fieldCount), 0)
	}
	return assembleClassAttrs(major, accessFlags, thisClass, superClass, cpClasses, cpStrings, interfaces, int64(fieldCount), int64(methodCount))
}

func assembleClassAttrs(major, accessFlags, thisClass, superClass uint16,
	cpClasses map[uint16]uint16, cpStrings map[uint16]string, interfaces []string,
	fieldCount, methodCount int64,
) Attributes {
	extras := Attributes{
		"method_count": methodCount,
		"field_count":  fieldCount,
		"access_flags": decodeClassAccessFlags(accessFlags),
	}
	if name := resolveClassName(thisClass, cpClasses, cpStrings); name != "" {
		extras["class_name"] = name
	}
	if name := resolveClassName(superClass, cpClasses, cpStrings); name != "" {
		extras["super_class"] = name
	}
	if len(interfaces) > 0 {
		extras["interfaces"] = interfaces
	}
	return bytecodeAttrs("jvm", javaVersion(major), extras)
}

// resolveClassName follows the CONSTANT_Class → name_index →
// CONSTANT_Utf8 chain to a Go string. Returns "" on any failure
// (out-of-bounds index, wrong tag, missing UTF-8 entry).
func resolveClassName(idx uint16, cpClasses map[uint16]uint16, cpStrings map[uint16]string) string {
	if idx == 0 {
		return ""
	}
	nameIdx, ok := cpClasses[idx]
	if !ok {
		return ""
	}
	return cpStrings[nameIdx]
}

// javaVersion maps the .class major version to its Java SE release
// name. Table per JVMS §4.1 — coverage from Java 1.1 through 24.
// Unknown versions surface as "class format N" so newer JDKs still
// produce a useful string.
func javaVersion(major uint16) string {
	switch major {
	case 45:
		return "Java 1.1"
	case 46:
		return "Java 1.2"
	case 47:
		return "Java 1.3"
	case 48:
		return "Java 1.4"
	case 49:
		return "Java 5"
	case 50:
		return "Java 6"
	case 51:
		return "Java 7"
	case 52:
		return "Java 8"
	case 53:
		return "Java 9"
	case 54:
		return "Java 10"
	case 55:
		return "Java 11"
	case 56:
		return "Java 12"
	case 57:
		return "Java 13"
	case 58:
		return "Java 14"
	case 59:
		return "Java 15"
	case 60:
		return "Java 16"
	case 61:
		return "Java 17"
	case 62:
		return "Java 18"
	case 63:
		return "Java 19"
	case 64:
		return "Java 20"
	case 65:
		return "Java 21"
	case 66:
		return "Java 22"
	case 67:
		return "Java 23"
	case 68:
		return "Java 24"
	}
	if major == 0 {
		return ""
	}
	return fmt.Sprintf("class format %d", major)
}

// decodeClassAccessFlags expands the u2 access_flags bitfield into a
// list of canonical strings ("public", "abstract", etc.).
func decodeClassAccessFlags(flags uint16) []string {
	out := make([]string, 0, 4)
	if flags&accPublic != 0 {
		out = append(out, "public")
	}
	if flags&accFinal != 0 {
		out = append(out, "final")
	}
	if flags&accSuper != 0 {
		out = append(out, "super")
	}
	if flags&accInterface != 0 {
		out = append(out, "interface")
	}
	if flags&accAbstract != 0 {
		out = append(out, "abstract")
	}
	if flags&accSynthetic != 0 {
		out = append(out, "synthetic")
	}
	if flags&accAnnotation != 0 {
		out = append(out, "annotation")
	}
	if flags&accEnum != 0 {
		out = append(out, "enum")
	}
	if flags&accModule != 0 {
		out = append(out, "module")
	}
	return out
}

// classReader is a bounds-checking cursor over a .class byte buffer.
// Every read returns (value, ok=false) on EOF so callers bail
// cleanly rather than panicking on out-of-bounds.
type classReader struct {
	data []byte
	pos  int
}

func (r *classReader) checkMagic() bool {
	v, ok := r.u4()
	return ok && v == classMagic
}

func (r *classReader) u1() (uint8, bool) {
	if r.pos+1 > len(r.data) {
		return 0, false
	}
	v := r.data[r.pos]
	r.pos++
	return v, true
}

func (r *classReader) u2() (uint16, bool) {
	if r.pos+2 > len(r.data) {
		return 0, false
	}
	v := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, true
}

func (r *classReader) u4() (uint32, bool) {
	if r.pos+4 > len(r.data) {
		return 0, false
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, true
}

func (r *classReader) advance(n int) bool {
	if n < 0 || r.pos+n > len(r.data) {
		return false
	}
	r.pos += n
	return true
}

// constantPool walks the constant pool and returns:
//   - cpStrings: CP index → UTF-8 string (CONSTANT_Utf8 entries)
//   - cpClasses: CP index → name_index (CONSTANT_Class entries point
//     at a CONSTANT_Utf8 via name_index — caller chains the lookup)
//
// Long and Double take TWO constant-pool slots (JVMS §4.4.5) — the
// extra i++ inside their case is correct, not a bug.
func (r *classReader) constantPool() (map[uint16]string, map[uint16]uint16, bool) {
	count, ok := r.u2()
	if !ok || int(count) > classMaxPoolEntries {
		return nil, nil, false
	}
	cpStrings := make(map[uint16]string, count)
	cpClasses := make(map[uint16]uint16, 16)
	for i := uint16(1); i < count; i++ {
		tag, ok := r.u1()
		if !ok {
			return nil, nil, false
		}
		switch tag {
		case cpTagUtf8:
			length, ok := r.u2()
			if !ok {
				return nil, nil, false
			}
			if int(length) > classMaxStringBytes || r.pos+int(length) > len(r.data) {
				return nil, nil, false
			}
			cpStrings[i] = string(r.data[r.pos : r.pos+int(length)])
			r.pos += int(length)
		case cpTagInteger, cpTagFloat:
			if !r.advance(4) {
				return nil, nil, false
			}
		case cpTagLong, cpTagDouble:
			if !r.advance(8) {
				return nil, nil, false
			}
			i++ // takes two pool slots
		case cpTagClass:
			nameIdx, ok := r.u2()
			if !ok {
				return nil, nil, false
			}
			cpClasses[i] = nameIdx
		case cpTagString, cpTagMethodType, cpTagModule, cpTagPackage:
			if !r.advance(2) {
				return nil, nil, false
			}
		case cpTagFieldref, cpTagMethodref, cpTagInterfaceMethodref,
			cpTagNameAndType, cpTagDynamic, cpTagInvokeDynamic:
			if !r.advance(4) {
				return nil, nil, false
			}
		case cpTagMethodHandle:
			if !r.advance(3) {
				return nil, nil, false
			}
		default:
			// Unknown tag — file is corrupt or uses a CP entry from
			// a JVM spec newer than we know. Bail cleanly.
			return nil, nil, false
		}
	}
	return cpStrings, cpClasses, true
}

// skipMembers advances past a fields[] or methods[] array. Each
// member has: u2 access + u2 name + u2 descriptor + u2 attr_count
// + attr_count × (u2 attribute_name + u4 attribute_length +
// attribute_length payload bytes). We don't decode payloads.
func (r *classReader) skipMembers(count int) bool {
	for range count {
		if !r.advance(6) {
			return false
		}
		attrCount, ok := r.u2()
		if !ok {
			return false
		}
		for range int(attrCount) {
			if !r.advance(2) {
				return false
			}
			attrLen, ok := r.u4()
			if !ok {
				return false
			}
			if !r.advance(int(attrLen)) {
				return false
			}
		}
	}
	return true
}
