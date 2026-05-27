package content

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"io/fs"
	"strconv"
	"strings"
)

// VOTable (IVOA tabular standard) constants. Per the VOTable 1.5
// specification (2024-09-18) the root element is <VOTABLE> with a
// version attribute and a namespace URI in the IVOA hierarchy.
const (
	// votableReadCap bounds how many bytes readVOTableInfo pulls off
	// disk. FIELD definitions live in the file header; we never walk
	// into <TABLEDATA> / <BINARY> rows. 1 MiB is generous for header
	// metadata across the largest catalogs (Gaia query results have
	// at most a few hundred FIELD declarations).
	votableReadCap = 1 * 1024 * 1024

	// votableMaxFields caps the per-file FIELD list to defend against
	// adversarial inputs claiming millions of columns. Real catalogs
	// have ≤ 100 columns; 10000 is way past that.
	votableMaxFields = 10000

	// votableNamespacePrefix is the IVOA namespace stem. Real-world
	// VOTable namespaces are versioned (.../v1.3, .../v1.4, .../v1.5)
	// so we prefix-match rather than equality-match.
	votableNamespacePrefix = "http://www.ivoa.net/xml/VOTable"
)

func init() {
	Register(&votableType{})
}

// votableType registers the science/votable content type. Detection
// is extension-only: VOTable files use .vot or .votable conventionally,
// and the registry's longest-suffix matcher picks these over the
// generic .xml extension when both are registered. Files literally
// named .xml that happen to contain VOTable XML detect as xml/xml
// today — namespace-aware fallback is a follow-up. The `<?xml` magic
// prefix would over-fire for plain XML and is left to the xml/xml
// type.
type votableType struct{}

func (v *votableType) Name() string       { return "science/votable" }
func (v *votableType) Extensions() []string { return []string{".vot", ".votable"} }
func (v *votableType) MagicBytes() [][]byte { return nil }

// Attributes parses the VOTable header. Streaming XML walk that
// stops at the first table's <DATA> child element (so we never read
// row payloads). Truncated / non-VOTable XML returns empty attrs.
func (v *votableType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readVOTableInfo(fsys, path)
}

func readVOTableInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, votableReadCap))
	if err != nil {
		return Attributes{}, nil
	}
	return parseVOTableHeader(buf), nil
}

// votableState accumulates parser results across the streaming walk.
// title is collected separately in parseVOTableHeader's titleBuf so
// CharData appends don't allocate intermediate strings.
type votableState struct {
	version    string
	tableCount int64
	totalRows  int64
	fieldNames []string
	fieldUnits []string
	fieldUCDs  []string
	dataFormat string // first table's DATA child name, lower-cased
	author     string
}

// parseVOTableHeader is the pure-function parser exercised by tests +
// fuzz. Walks the XML token stream collecting:
//   - VOTABLE version attribute
//   - INFO@name="creator" → author
//   - top-level DESCRIPTION CharData → title
//   - TABLE nrows attribute (summed across tables)
//   - FIELD name / unit / ucd attributes (parallel lists)
//   - first TABLE's DATA child element name (tabledata / binary /
//     binary2 / fits)
//
// DATA child contents are skipped via decoder.Skip() so row payloads
// don't drive token count. Namespace check: accepts any URI starting
// with the IVOA VOTable prefix (covers v1.2/1.3/1.4/1.5).
func parseVOTableHeader(data []byte) Attributes {
	state := votableState{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var (
		tokens          int
		rootSeen        bool
		depth           int
		inDescription   bool
		descDepth       int
		titleBuf        strings.Builder
	)

	for tokens < maxXMLTokens {
		tokens++
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			local := t.Name.Local

			if !rootSeen {
				if local != "VOTABLE" {
					return Attributes{}
				}
				// Namespace check — accept any IVOA VOTable namespace
				// version, plus the empty namespace (some hand-written
				// files omit xmlns).
				if t.Name.Space != "" && !strings.HasPrefix(t.Name.Space, votableNamespacePrefix) {
					return Attributes{}
				}
				rootSeen = true
				for _, a := range t.Attr {
					if a.Name.Local == "version" {
						state.version = a.Value
					}
				}
				continue
			}

			switch local {
			case "DESCRIPTION":
				// Only the DESCRIPTION at depth 2 (direct child of
				// VOTABLE) is the document title — nested DESCRIPTIONs
				// (inside RESOURCE / TABLE / FIELD) describe the
				// enclosing element, not the document.
				if depth == 2 {
					inDescription = true
					descDepth = depth
				}
			case "INFO":
				var iname, ivalue string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "name":
						iname = a.Value
					case "value":
						ivalue = a.Value
					}
				}
				if iname == "creator" && ivalue != "" {
					state.author = ivalue
				}
			case "TABLE":
				state.tableCount++
				for _, a := range t.Attr {
					if a.Name.Local == "nrows" {
						if n, err := strconv.ParseInt(a.Value, 10, 64); err == nil && n >= 0 {
							state.totalRows += n
						}
					}
				}
			case "FIELD":
				if int64(len(state.fieldNames)) < votableMaxFields {
					var name, unit, ucd string
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "name":
							name = a.Value
						case "unit":
							unit = a.Value
						case "ucd":
							ucd = a.Value
						}
					}
					state.fieldNames = append(state.fieldNames, name)
					state.fieldUnits = append(state.fieldUnits, unit)
					state.fieldUCDs = append(state.fieldUCDs, ucd)
				}
			case "TABLEDATA", "BINARY", "BINARY2", "FITS":
				// Capture the first table's data format then fast-forward
				// past the data payload — we never walk into TR/TD or
				// binary blobs.
				if state.tableCount == 1 && state.dataFormat == "" {
					state.dataFormat = strings.ToLower(local)
				}
				if err := dec.Skip(); err != nil {
					// Skip past current element. Skip returns when the
					// matching EndElement is consumed; on error (malformed
					// XML) we bail.
					return assembleVOTableAttrs(state, titleBuf.String())
				}
				depth-- // Skip() consumed our EndElement
			}

		case xml.EndElement:
			depth--
			if inDescription && depth < descDepth {
				inDescription = false
			}

		case xml.CharData:
			if inDescription {
				titleBuf.Write(t)
			}
		}
	}

	if !rootSeen {
		return Attributes{}
	}
	return assembleVOTableAttrs(state, titleBuf.String())
}

// assembleVOTableAttrs packs the collected state into the CEL surface
// via the shared scienceAttrs factory. Lifts title from the trimmed
// DESCRIPTION CharData; author is whatever the most recent
// INFO@name="creator" value was.
func assembleVOTableAttrs(s votableState, title string) Attributes {
	extras := Attributes{
		"votable_version":     s.version,
		"table_count":         s.tableCount,
		"total_rows":          s.totalRows,
		"votable_data_format": s.dataFormat,
	}
	if len(s.fieldNames) > 0 {
		extras["field_names"] = s.fieldNames
		extras["field_units"] = s.fieldUnits
		extras["field_ucds"] = s.fieldUCDs
	}
	if t := strings.TrimSpace(title); t != "" {
		extras["title"] = t
	}
	if s.author != "" {
		extras["author"] = s.author
	}
	return scienceAttrs("votable", extras)
}
