package content

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"
)

// PDS (NASA Planetary Data System) constants.
//
// Two variants ship under the same `pdsType` struct:
//
//   - PDS3 (legacy — Voyager through Curiosity) uses PVL labels:
//     free-form `KEYWORD = VALUE` records inside `.lbl` files,
//     terminated by `END`. Magic prefix `PDS_VERSION_ID` lets the
//     first-512-byte sniffer catch them regardless of extension.
//
//   - PDS4 (current — Perseverance, Lucy, future missions) uses
//     XML labels in the NASA PDS4 namespace, typically named
//     `.lblx`. We do NOT register `.xml` because the generic xml
//     content type already owns it; PDS4 files literally named
//     `.xml` detect as plain XML.
const (
	pdsReadCap = 1 * 1024 * 1024

	pds4NamespacePrefix = "http://pds.nasa.gov/pds4/pds"
)

func init() {
	Register(&pdsType{
		name:  "science/pds3",
		exts:  []string{".lbl"},
		magic: [][]byte{[]byte("PDS_VERSION_ID")},
	})
	Register(&pdsType{
		name:  "science/pds4",
		exts:  []string{".lblx"},
		magic: nil,
	})
}

// pdsType handles both PDS3 (PVL label) and PDS4 (XML label) variants
// via the same `Name() / Extensions() / MagicBytes() / Attributes()`
// surface. Per-variant dispatch in Attributes() switches on p.name,
// mirroring the `bytecodetype.go` / `diskimagetype.go` pattern.
type pdsType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (p *pdsType) Name() string         { return p.name }
func (p *pdsType) Extensions() []string { return p.exts }
func (p *pdsType) MagicBytes() [][]byte { return p.magic }

func (p *pdsType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, pdsReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	switch p.name {
	case "science/pds3":
		return parsePDS3Label(buf), nil
	case "science/pds4":
		return parsePDS4Label(buf), nil
	}
	return nil, errors.New("unsupported pds type")
}

// stripPVLComments removes PVL `/* ... */` block comments from the
// input. PDS3 labels use these liberally for descriptive notes; my
// parser doesn't care about them. Cheap pre-pass since labels are
// small (< 1 MiB cap).
func stripPVLComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if i+1 < len(data) && data[i] == '/' && data[i+1] == '*' {
			// Skip until matching */
			j := i + 2
			for j+1 < len(data) {
				if data[j] == '*' && data[j+1] == '/' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}
		out = append(out, data[i])
		i++
	}
	return out
}

// stripPVLQuotes removes surrounding double-quotes from a PVL value
// (`"foo bar"` → `foo bar`). Leaves unquoted values untouched.
func stripPVLQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parsePDS3Label walks a PDS3 PVL label as line-oriented
// `KEYWORD = VALUE` records. Comments stripped first. Multi-line
// values (continuation via trailing `\`) and OBJECT/END_OBJECT
// nested groups are out of scope for v1 — most metadata we care
// about lives at the top level. Unknown / unsupported lines pass
// through silently.
func parsePDS3Label(data []byte) Attributes {
	cleaned := stripPVLComments(data)

	out := Attributes{}
	var (
		pdsVersion     string
		missionName    string
		spacecraftName string
		instrumentName string
		targetName     string
		productID      string
		startTime      string
	)

	scanner := bufio.NewScanner(bytes.NewReader(cleaned))
	scanner.Buffer(make([]byte, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		val = stripPVLQuotes(val)

		switch strings.ToUpper(key) {
		case "PDS_VERSION_ID":
			pdsVersion = val
		case "MISSION_NAME":
			missionName = val
		case "SPACECRAFT_NAME", "INSTRUMENT_HOST_NAME":
			if spacecraftName == "" {
				spacecraftName = val
			}
		case "INSTRUMENT_NAME", "INSTRUMENT_ID":
			if instrumentName == "" {
				instrumentName = val
			}
		case "TARGET_NAME":
			targetName = val
		case "PRODUCT_ID":
			productID = val
		case "START_TIME":
			startTime = val
		}
	}

	// Nothing parsed → empty attrs (acceptable: file was magic-
	// detected but the body was unparseable).
	if pdsVersion == "" && missionName == "" && targetName == "" && productID == "" {
		return Attributes{}
	}

	if pdsVersion != "" {
		out["pds_version"] = pdsVersion
	}
	assignPDSCommon(out, missionName, spacecraftName, instrumentName, targetName, productID, startTime)
	return scienceAttrs("pds3", out)
}

// pds4Product captures the slice of PDS4 schema we care about.
// encoding/xml's Unmarshal maps elements by local name, ignoring
// namespace — fine here because the XMLName.Space check on the
// root element gates whether we trust the document at all.
type pds4Product struct {
	XMLName            xml.Name
	IdentificationArea struct {
		LogicalIdentifier string `xml:"logical_identifier"`
		Title             string `xml:"title"`
	} `xml:"Identification_Area"`
	ObservationArea struct {
		TimeCoordinates struct {
			StartDateTime string `xml:"start_date_time"`
		} `xml:"Time_Coordinates"`
		InvestigationArea struct {
			Name string `xml:"name"`
		} `xml:"Investigation_Area"`
		ObservingSystem struct {
			Components []struct {
				Name string `xml:"name"`
				Type string `xml:"type"`
			} `xml:"Observing_System_Component"`
		} `xml:"Observing_System"`
		TargetIdentification struct {
			Name string `xml:"name"`
		} `xml:"Target_Identification"`
	} `xml:"Observation_Area"`
}

// parsePDS4Label parses a PDS4 XML label via encoding/xml's
// struct-tag Unmarshal. Cheap because labels are < 1 MiB. The
// XMLName.Space check on the root rejects non-PDS4 XML; only
// Product_Observational is supported in v1 (Product_Bundle /
// Product_Collection / Product_Document have different schemas and
// would each warrant their own struct).
func parsePDS4Label(data []byte) Attributes {
	var prod pds4Product
	if err := xml.Unmarshal(data, &prod); err != nil {
		return Attributes{}
	}
	// Reject non-PDS4 XML by checking the namespace AND root local
	// name. Other Product_* variants (Bundle / Collection / Document)
	// have different observation-area shapes and are out of scope.
	if !strings.HasPrefix(prod.XMLName.Space, pds4NamespacePrefix) {
		return Attributes{}
	}
	if prod.XMLName.Local != "Product_Observational" {
		return Attributes{}
	}

	// Walk Observing_System_Components: first Host/Spacecraft maps
	// to spacecraft_name; first Instrument maps to instrument_name.
	var spacecraftName, instrumentName string
	for _, osc := range prod.ObservationArea.ObservingSystem.Components {
		switch osc.Type {
		case "Host", "Spacecraft":
			if spacecraftName == "" {
				spacecraftName = osc.Name
			}
		case "Instrument":
			if instrumentName == "" {
				instrumentName = osc.Name
			}
		}
	}

	out := Attributes{
		"pds_version": "PDS4",
	}
	if t := strings.TrimSpace(prod.IdentificationArea.Title); t != "" {
		out["title"] = t
	}
	assignPDSCommon(out,
		strings.TrimSpace(prod.ObservationArea.InvestigationArea.Name),
		spacecraftName,
		instrumentName,
		strings.TrimSpace(prod.ObservationArea.TargetIdentification.Name),
		strings.TrimSpace(prod.IdentificationArea.LogicalIdentifier),
		strings.TrimSpace(prod.ObservationArea.TimeCoordinates.StartDateTime),
	)
	return scienceAttrs("pds4", out)
}

// assignPDSCommon populates the cross-variant attributes (everything
// except pds_version and title-from-PDS4). For PDS3 the title is
// synthesised from instrument + target; PDS4 lifts the explicit
// <title> in Identification_Area.
func assignPDSCommon(out Attributes, missionName, spacecraftName, instrumentName, targetName, productID, startTime string) {
	if missionName != "" {
		out["mission_name"] = missionName
	}
	if spacecraftName != "" {
		out["spacecraft_name"] = spacecraftName
	}
	if instrumentName != "" {
		out["instrument_name"] = instrumentName
	}
	if targetName != "" {
		out["target_name"] = targetName
	}
	if productID != "" {
		out["product_id"] = productID
	}
	if startTime != "" {
		out["start_time"] = startTime
		if t, ok := parsePDSDate(startTime); ok {
			out["taken_at"] = t
		}
	}
	// PDS3 lacks an explicit title field — synthesise one from
	// INSTRUMENT_NAME + TARGET_NAME so cross-family `title.contains`
	// queries work. Only fires when at least one of the two is set
	// AND `title` hasn't already been set by the caller (PDS4
	// populates title explicitly before this is called).
	if _, hasTitle := out["title"]; !hasTitle {
		if instrumentName != "" || targetName != "" {
			parts := []string{}
			if instrumentName != "" {
				parts = append(parts, instrumentName)
			}
			if targetName != "" {
				parts = append(parts, targetName)
			}
			out["title"] = strings.Join(parts, " ")
		}
	}
}

// parsePDSDate parses a PDS START_TIME / start_date_time value. PDS3
// values often omit the `Z` UTC suffix; PDS4 follows strict ISO 8601.
// We try the common shapes; failure surfaces as `taken_at` unset.
func parsePDSDate(s string) (time.Time, bool) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	s = strings.TrimSpace(s)
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
