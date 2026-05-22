package content

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SQLite b-tree page walker for sqlite_master schema introspection
// (follow-up to #170 / #174). Pure stdlib, no third-party SQLite
// library. Bounded by design — every loop has a cap, every pointer
// is bounds-checked, overflow page chains are skipped (documented).
//
// Reads page 1's sqlite_master b-tree and surfaces:
//   - sqlite_table_count / _view_count / _index_count / _trigger_count
//   - sqlite_table_names — sorted, deduplicated, capped at 100
//   - sqlite_schema_fingerprint — SHA256 hex over sorted (type, name, sql)
//
// Format references: https://www.sqlite.org/fileformat.html §1.5, §1.6, §2.
const (
	// Defensive caps. Real schemas have ~10-100 sqlite_master rows
	// even for big apps; 10000 is generous and bounds an adversarial
	// input that claims hundreds of millions of objects.
	sqliteMaxBtreeDepth   = 8
	sqliteMaxObjects      = 10000
	sqliteMaxRecordHeader = 256
	sqliteMaxColumnBytes  = 65536
	sqliteMaxTableNames   = 100

	// B-tree page type bytes per SQLite §1.6.
	sqliteBtreeLeafTable     = 0x0D
	sqliteBtreeInteriorTable = 0x05
	// Index-page types we deliberately don't walk — sqlite_master is
	// always a table b-tree.
	// sqliteBtreeLeafIndex     = 0x0A
	// sqliteBtreeInteriorIndex = 0x02
)

// sqliteMasterRow is the slice of an sqlite_master row we care about:
// the type discriminator, the object name, and the original CREATE
// statement. Other columns (tbl_name, rootpage) are skipped by the
// record decoder.
type sqliteMasterRow struct {
	Type string // "table" / "view" / "index" / "trigger"
	Name string
	SQL  string
}

// decodeVarint decodes a SQLite-style varint per §1.5. Returns the
// decoded value and the number of bytes consumed; (0, 0) on
// underflow. Varints are 1-9 bytes:
//
//   - Bytes 0-7 use 7 bits each (high bit is the continuation flag).
//   - The 9th byte uses all 8 bits.
func decodeVarint(data []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 8 && i < len(data); i++ {
		b := data[i]
		v = (v << 7) | uint64(b&0x7F)
		if b&0x80 == 0 {
			return v, i + 1
		}
	}
	if len(data) < 9 {
		return 0, 0
	}
	v = (v << 8) | uint64(data[8])
	return v, 9
}

// recordKind discriminates the result of decoding one SQLite record
// column. Only kinds we surface get full handling — float64s in
// sqlite_master are unexpected and parse to recordNull effectively.
type recordKind int

const (
	recordNull recordKind = iota
	recordInt
	recordFloat
	recordBlob
	recordText
)

// recordVal is one decoded record column.
type recordVal struct {
	Kind  recordKind
	Int   int64
	Bytes []byte // for text and blob; text is UTF-8
}

// decodeRecord parses a SQLite record (header + body) and returns
// the columns listed in wantedCols (ascending order). wantedCols
// MUST be sorted ascending; unsorted input produces undefined
// results.
//
// The header is a varint of header-size followed by per-column
// serial-type varints (§2.1). The body is the columns concatenated
// per the serial-type encoding.
//
// We don't decode columns we don't want — we just skip past them
// using columnSize() — which keeps the walker cheap for sparse
// projections (sqlite_master has 5 columns and we want 3).
func decodeRecord(payload []byte, wantedCols []int) ([]recordVal, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty record")
	}
	headerSize, n := decodeVarint(payload)
	if n == 0 || int(headerSize) > len(payload) || headerSize > sqliteMaxRecordHeader || headerSize < uint64(n) {
		return nil, errors.New("record header out of bounds")
	}

	// Walk the column type list, also building a positional table.
	// Done up-front so we can skip unwanted columns cheaply.
	var serialTypes []uint64
	pos := n
	for pos < int(headerSize) {
		t, m := decodeVarint(payload[pos:int(headerSize)])
		if m == 0 {
			return nil, errors.New("truncated record header")
		}
		serialTypes = append(serialTypes, t)
		pos += m
	}

	out := make([]recordVal, len(wantedCols))
	bodyPos := int(headerSize)
	wantedIdx := 0
	for col, st := range serialTypes {
		if wantedIdx < len(wantedCols) && wantedCols[wantedIdx] == col {
			val, consumed, err := readColumn(payload[bodyPos:], st)
			if err != nil {
				return out, err
			}
			out[wantedIdx] = val
			wantedIdx++
			bodyPos += consumed
		} else {
			size := columnSize(st)
			if size > sqliteMaxColumnBytes {
				return out, errors.New("column size exceeds cap")
			}
			bodyPos += size
			if bodyPos > len(payload) {
				return out, errors.New("record body truncated")
			}
		}
		if wantedIdx >= len(wantedCols) {
			break
		}
	}
	return out, nil
}

// readColumn extracts one column value from payload starting at
// offset 0. Returns the value, the number of bytes consumed, and
// error. Per §2.1 the serial type encodes both the column type and
// (for variable-length kinds) the byte length.
func readColumn(data []byte, serialType uint64) (recordVal, int, error) {
	switch {
	case serialType == 0:
		return recordVal{Kind: recordNull}, 0, nil
	case serialType == 1:
		if len(data) < 1 {
			return recordVal{}, 0, errors.New("u8 truncated")
		}
		return recordVal{Kind: recordInt, Int: int64(int8(data[0]))}, 1, nil
	case serialType == 2:
		if len(data) < 2 {
			return recordVal{}, 0, errors.New("u16 truncated")
		}
		return recordVal{Kind: recordInt, Int: int64(int16(binary.BigEndian.Uint16(data)))}, 2, nil
	case serialType == 3:
		if len(data) < 3 {
			return recordVal{}, 0, errors.New("u24 truncated")
		}
		v := int64(data[0])<<16 | int64(data[1])<<8 | int64(data[2])
		if v&0x800000 != 0 {
			v -= 0x1000000
		}
		return recordVal{Kind: recordInt, Int: v}, 3, nil
	case serialType == 4:
		if len(data) < 4 {
			return recordVal{}, 0, errors.New("u32 truncated")
		}
		return recordVal{Kind: recordInt, Int: int64(int32(binary.BigEndian.Uint32(data)))}, 4, nil
	case serialType == 5:
		if len(data) < 6 {
			return recordVal{}, 0, errors.New("u48 truncated")
		}
		v := int64(data[0])<<40 | int64(data[1])<<32 | int64(data[2])<<24 |
			int64(data[3])<<16 | int64(data[4])<<8 | int64(data[5])
		if v&0x800000000000 != 0 {
			v -= 0x1000000000000
		}
		return recordVal{Kind: recordInt, Int: v}, 6, nil
	case serialType == 6:
		if len(data) < 8 {
			return recordVal{}, 0, errors.New("u64 truncated")
		}
		return recordVal{Kind: recordInt, Int: int64(binary.BigEndian.Uint64(data))}, 8, nil
	case serialType == 7:
		// IEEE float64 — we don't decode the value but must consume
		// 8 bytes so the body cursor stays aligned.
		if len(data) < 8 {
			return recordVal{}, 0, errors.New("float truncated")
		}
		return recordVal{Kind: recordFloat}, 8, nil
	case serialType == 8:
		return recordVal{Kind: recordInt, Int: 0}, 0, nil
	case serialType == 9:
		return recordVal{Kind: recordInt, Int: 1}, 0, nil
	case serialType >= 12 && serialType%2 == 0:
		size := int((serialType - 12) / 2)
		if size > sqliteMaxColumnBytes || size > len(data) {
			return recordVal{}, 0, errors.New("blob too large or truncated")
		}
		return recordVal{Kind: recordBlob, Bytes: data[:size]}, size, nil
	case serialType >= 13 && serialType%2 == 1:
		size := int((serialType - 13) / 2)
		if size > sqliteMaxColumnBytes || size > len(data) {
			return recordVal{}, 0, errors.New("text too large or truncated")
		}
		return recordVal{Kind: recordText, Bytes: data[:size]}, size, nil
	}
	return recordVal{}, 0, fmt.Errorf("unknown serial type %d", serialType)
}

// columnSize returns the byte width to skip for a column we don't
// want to decode. Mirrors readColumn's size logic.
func columnSize(serialType uint64) int {
	switch serialType {
	case 0, 8, 9:
		return 0
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 6
	case 6, 7:
		return 8
	}
	if serialType >= 12 && serialType%2 == 0 {
		return int((serialType - 12) / 2)
	}
	if serialType >= 13 && serialType%2 == 1 {
		return int((serialType - 13) / 2)
	}
	return 0
}

// walkSQLiteMaster walks the sqlite_master b-tree starting from
// page 1 and calls visit for each row. Best-effort — malformed
// pages mid-walk are skipped (the caller gets whatever was
// successfully visited).
func walkSQLiteMaster(data []byte, pageSize int, visit func(row sqliteMasterRow)) error {
	if pageSize <= 0 {
		return errors.New("non-positive page size")
	}
	if len(data) < pageSize {
		return errors.New("data shorter than one page")
	}
	visited := 0
	return walkSQLiteMasterPage(data, 1, pageSize, 0, &visited, visit)
}

// walkSQLiteMasterPage walks a single b-tree page identified by
// pageNum (1-indexed). For leaf pages it decodes cells and calls
// visit; for interior pages it recurses into child pages.
func walkSQLiteMasterPage(data []byte, pageNum, pageSize, depth int, visited *int, visit func(row sqliteMasterRow)) error {
	if depth >= sqliteMaxBtreeDepth {
		return errors.New("max btree depth exceeded")
	}
	if pageNum < 1 {
		return errors.New("invalid page number")
	}
	if *visited >= sqliteMaxObjects {
		return nil
	}

	pageOffset := (pageNum - 1) * pageSize
	if pageOffset < 0 || pageOffset+pageSize > len(data) {
		return errors.New("page out of bounds")
	}
	page := data[pageOffset : pageOffset+pageSize]

	// Page 1 has the 100-byte file header preceding the b-tree
	// page; the b-tree page header starts at byte offset 100.
	headerOffset := 0
	if pageNum == 1 {
		headerOffset = 100
	}
	if headerOffset+8 > len(page) {
		return errors.New("page header truncated")
	}
	btree := page[headerOffset:]

	pageType := btree[0]
	numCells := int(binary.BigEndian.Uint16(btree[3:5]))

	switch pageType {
	case sqliteBtreeLeafTable:
		ptrsOffset := 8
		for range numCells {
			if *visited >= sqliteMaxObjects {
				return nil
			}
			if ptrsOffset+2 > len(btree) {
				return nil // cell-pointer array overruns page — bail
			}
			cellOffset := int(binary.BigEndian.Uint16(btree[ptrsOffset : ptrsOffset+2]))
			ptrsOffset += 2

			// cellOffset is relative to the PAGE start (not the
			// b-tree-page-header start), even on page 1.
			if cellOffset < 0 || cellOffset >= len(page) {
				continue
			}
			cell := page[cellOffset:]
			processLeafTableCell(cell, visited, visit)
		}
		return nil

	case sqliteBtreeInteriorTable:
		if len(btree) < 12 {
			return errors.New("interior page header truncated")
		}
		rightMost := int(binary.BigEndian.Uint32(btree[8:12]))

		ptrsOffset := 12
		for range numCells {
			if *visited >= sqliteMaxObjects {
				return nil
			}
			if ptrsOffset+2 > len(btree) {
				break
			}
			cellOffset := int(binary.BigEndian.Uint16(btree[ptrsOffset : ptrsOffset+2]))
			ptrsOffset += 2
			if cellOffset < 0 || cellOffset+4 > len(page) {
				continue
			}
			childPage := int(binary.BigEndian.Uint32(page[cellOffset : cellOffset+4]))
			// Best-effort: a bad child page is skipped, walk continues.
			_ = walkSQLiteMasterPage(data, childPage, pageSize, depth+1, visited, visit)
		}
		// Right-most child pointer; walked even when numCells == 0.
		if rightMost > 0 {
			_ = walkSQLiteMasterPage(data, rightMost, pageSize, depth+1, visited, visit)
		}
		return nil
	}

	// Unknown page type (e.g. an index b-tree). sqlite_master should
	// always be a table b-tree; if we land on something else the file
	// is corrupt or we walked into a wrong page. Skip silently.
	return nil
}

// processLeafTableCell decodes one cell payload from a leaf table
// page. SQLite cells in leaf table pages have the shape:
//
//	varint payload-size
//	varint rowid
//	bytes  record-payload (size = payload-size, inline portion only)
//	[4 bytes overflow page pointer if local < total]
//
// We skip overflow chains in v1 — the visit function sees only the
// inline portion of the record. For sqlite_master rows this is
// typically the full record because CREATE statements fit inline.
func processLeafTableCell(cell []byte, visited *int, visit func(row sqliteMasterRow)) {
	payloadSize, n1 := decodeVarint(cell)
	if n1 == 0 {
		return
	}
	_, n2 := decodeVarint(cell[n1:])
	if n2 == 0 {
		return
	}
	payloadStart := n1 + n2

	// SQLite computes the local payload size via a formula involving
	// the usable page size, max-local, and min-local thresholds. For
	// sqlite_master rows that fit inline (common case), the formula
	// reduces to "all of payload-size". For overflow cases we'd
	// follow a 4-byte page pointer; v1 just clamps to available
	// bytes — a deliberately conservative choice that may truncate
	// the SQL string for very long CREATE statements (documented).
	local := min(int(payloadSize), len(cell)-payloadStart)
	if local <= 0 {
		return
	}
	payload := cell[payloadStart : payloadStart+local]

	// sqlite_master columns are (type, name, tbl_name, rootpage, sql).
	// We want columns 0, 1, 4 — skip tbl_name and rootpage.
	vals, err := decodeRecord(payload, []int{0, 1, 4})
	if err != nil || len(vals) < 3 {
		return
	}
	row := sqliteMasterRow{
		Type: textOf(vals[0]),
		Name: textOf(vals[1]),
		SQL:  textOf(vals[2]),
	}
	*visited++
	visit(row)
}

// textOf returns the string contents of a record value, or "" if the
// value isn't a text column.
func textOf(v recordVal) string {
	if v.Kind == recordText {
		return string(v.Bytes)
	}
	return ""
}

// schemaFromSQLiteMaster aggregates sqlite_master rows into the six
// CEL attributes. Best-effort — any error from the walk is swallowed
// and we return whatever was collected before the failure.
func schemaFromSQLiteMaster(data []byte, pageSize int64) Attributes {
	var tables, views, indexes, triggers int64
	tableNames := make(map[string]bool)

	type entry = sqliteMasterRow
	var entries []entry

	visit := func(row sqliteMasterRow) {
		switch row.Type {
		case "table":
			tables++
			if len(tableNames) < sqliteMaxTableNames {
				tableNames[row.Name] = true
			}
		case "view":
			views++
		case "index":
			indexes++
		case "trigger":
			triggers++
		}
		entries = append(entries, entry(row))
	}
	_ = walkSQLiteMaster(data, int(pageSize), visit)

	out := Attributes{}
	if tables > 0 {
		out["sqlite_table_count"] = tables
	}
	if views > 0 {
		out["sqlite_view_count"] = views
	}
	if indexes > 0 {
		out["sqlite_index_count"] = indexes
	}
	if triggers > 0 {
		out["sqlite_trigger_count"] = triggers
	}
	if len(tableNames) > 0 {
		names := make([]string, 0, len(tableNames))
		for n := range tableNames {
			names = append(names, n)
		}
		sort.Strings(names)
		out["sqlite_table_names"] = names
	}
	if len(entries) > 0 {
		// Stable order: sort by (type, name) so cosmetic schema
		// reorders don't change the fingerprint.
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Type != entries[j].Type {
				return entries[i].Type < entries[j].Type
			}
			return entries[i].Name < entries[j].Name
		})
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(e.Type)
			sb.WriteByte(0)
			sb.WriteString(e.Name)
			sb.WriteByte(0)
			sb.WriteString(e.SQL)
			sb.WriteByte(1) // record separator
		}
		sum := sha256.Sum256([]byte(sb.String()))
		out["sqlite_schema_fingerprint"] = hex.EncodeToString(sum[:])
	}
	return out
}
