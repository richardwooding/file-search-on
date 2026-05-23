package content

import (
	"io"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"time"
)

// FITS file constants per the FITS Standard (NASA/Science Office of
// Standards and Technology, version 4.0). Headers are 80-byte ASCII
// "cards" packed into 2880-byte blocks; data units that follow each
// header are also padded to a 2880-byte boundary. The format hasn't
// changed materially since 1981 — these constants are immutable.
const (
	fitsBlockSize = 2880
	fitsCardSize  = 80
	fitsCardsPerBlock = fitsBlockSize / fitsCardSize // 36

	// fitsMaxHDUs caps the HDU walker. Real multi-extension FITS files
	// rarely exceed 20 HDUs (mosaic imagers like LSST cameras top out
	// around there); 100 is generous and defends against adversarial
	// inputs that claim NAXIS dimensions that loop the walker forever.
	fitsMaxHDUs = 100

	// fitsReadCap bounds how many bytes readFITSInfo pulls off disk.
	// A 2880-byte header per HDU × 100 HDUs is 288 KiB worst case, but
	// most files concentrate metadata in the primary HDU. 256 KiB
	// covers typical multi-extension files and stops adversarial
	// headers from forcing megabyte reads. Above the cap, hdu_count
	// reflects only what was reachable in the read window.
	fitsReadCap = 256 * 1024
)

// readFITSInfo opens the file, reads up to fitsReadCap bytes, and
// dispatches to the pure-function parser. Truncated files still
// produce best-effort attributes — corrupt headers degrade silently
// per the walker contract.
func readFITSInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, fitsReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseFITSHeaders(buf), nil
}

// parseFITSHeaders is the pure-function parser exercised by tests +
// fuzz. Walks the primary HDU header (cards until END), assembles the
// CEL-visible attribute map, then walks forward through any extension
// HDUs to count them.
func parseFITSHeaders(data []byte) Attributes {
	primary, headerEnd, ok := parseFITSHeader(data, 0)
	if !ok {
		return Attributes{}
	}
	hduCount := countHDUs(data, headerEnd, primary)
	return assembleFITSAttrs(primary, hduCount)
}

// parseFITSHeader parses the header beginning at offset `start`. Reads
// 80-byte cards until it sees an `END` card or runs out of data.
// Returns the keyword→value map, the byte offset immediately past the
// header (rounded up to a 2880-byte block boundary), and ok=false if
// the header is malformed enough to abandon (no SIMPLE/XTENSION card
// at the start, no END encountered within the read window, etc.).
func parseFITSHeader(data []byte, start int) (map[string]any, int, bool) {
	if start >= len(data) {
		return nil, start, false
	}
	out := make(map[string]any, 16)
	pos := start
	endSeen := false
	for pos+fitsCardSize <= len(data) {
		card := data[pos : pos+fitsCardSize]
		pos += fitsCardSize
		// Strip trailing whitespace for keyword check; the literal
		// `END     ` card is 80 bytes of "END" + 77 spaces.
		key, val, ok := parseCard(card)
		if !ok {
			// Either END, HISTORY/COMMENT, blank, or no `=` — these
			// don't contribute to the attribute map. We still need to
			// detect END to know the header is closed.
			trimmed := strings.TrimSpace(string(card))
			if trimmed == "END" || strings.HasPrefix(trimmed, "END ") || trimmed == "" {
				if trimmed == "END" || strings.HasPrefix(trimmed, "END ") {
					endSeen = true
					break
				}
			}
			continue
		}
		out[key] = val
	}
	if !endSeen {
		// No END card found in the read window. We still return what
		// we have if there's anything useful; callers gate on the
		// returned ok.
		if len(out) == 0 {
			return nil, pos, false
		}
	}
	// Round pos up to the next 2880-byte block boundary so the HDU
	// walker can start reading data immediately.
	headerEnd := roundUp(pos, fitsBlockSize)
	return out, headerEnd, true
}

// parseCard parses a single 80-byte card. Returns (keyword, value, ok)
// where ok=false signals a non-data card (END, COMMENT, HISTORY,
// blank, or missing `=` indicator). String values are stripped of
// surrounding single quotes and trailing whitespace; numeric values
// parse as int64 first then float64; T/F booleans become Go bools.
func parseCard(card []byte) (string, any, bool) {
	if len(card) < 10 {
		return "", nil, false
	}
	// Keyword: cols 1-8 (bytes 0-7), trimmed. Value indicator must be
	// `=` at col 9 (byte 8) followed by space at col 10 (byte 9) per
	// the FITS Standard §4.1.2.1.
	keyRaw := string(card[0:8])
	key := strings.TrimSpace(keyRaw)
	if key == "" || key == "END" || key == "COMMENT" || key == "HISTORY" {
		return "", nil, false
	}
	if card[8] != '=' {
		return "", nil, false
	}
	// Value: cols 11-80 (bytes 10-79), strip trailing comment after
	// `/` (if the slash is OUTSIDE a quoted string).
	valueField := string(card[10:])
	value := stripFITSComment(valueField)
	value = strings.TrimSpace(value)
	if value == "" {
		return key, "", true
	}
	// Quoted string?
	if value[0] == '\'' {
		// Find closing single quote. FITS uses '' to escape one '
		// inside a string, but we don't need to round-trip — just
		// strip the surrounding quotes and trim trailing blanks.
		end := strings.LastIndex(value, "'")
		if end <= 0 {
			return key, value, true
		}
		s := value[1:end]
		s = strings.TrimRight(s, " ")
		// Normalise '' → '
		s = strings.ReplaceAll(s, "''", "'")
		return key, s, true
	}
	// Boolean? Single T or F right-justified at col 30 typically, but
	// the value may be padded — check after trimming.
	if value == "T" {
		return key, true, true
	}
	if value == "F" {
		return key, false, true
	}
	// Numeric. Try int first (FITS BITPIX, NAXIS, NAXISn are integers
	// by spec) so consumers don't have to dance around float64 for
	// integer-shaped fields.
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return key, i, true
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return key, f, true
	}
	// Unparseable — surface as the raw trimmed string. Useful for
	// values FITS allows but we don't model (complex pairs, etc.).
	return key, value, true
}

// stripFITSComment removes `/ comment` text after the value, taking
// care to leave `/`s inside quoted strings alone.
func stripFITSComment(s string) string {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			inQuote = !inQuote
		case '/':
			if !inQuote {
				return s[:i]
			}
		}
	}
	return s
}

// countHDUs walks forward from the end of the primary header,
// advancing past each HDU's data unit to reach the next header.
// Returns at least 1 (the primary HDU exists by definition since we
// got past parseFITSHeader). Capped at fitsMaxHDUs.
func countHDUs(data []byte, primaryHeaderEnd int, primary map[string]any) int {
	count := 1
	pos := primaryHeaderEnd + dataUnitSize(primary)
	for count < fitsMaxHDUs && pos+fitsCardSize <= len(data) {
		header, headerEnd, ok := parseFITSHeader(data, pos)
		if !ok {
			break
		}
		count++
		pos = headerEnd + dataUnitSize(header)
		if pos <= primaryHeaderEnd {
			// Defensive: if dataUnitSize underflows we'd loop forever.
			break
		}
	}
	return count
}

// dataUnitSize computes the byte length of the data unit attached to
// a header, rounded up to a 2880-byte block boundary. Per FITS §4.4:
// data_bytes = |BITPIX|/8 * Π(NAXISᵢ). NAXIS=0 means no data unit.
// Bounded against integer overflow — products that would exceed
// MaxInt64/2 are clamped to a size that will overshoot the read
// window, terminating the walker via the pos > len(data) guard.
func dataUnitSize(header map[string]any) int {
	naxis := fitsInt(header["NAXIS"])
	if naxis <= 0 {
		return 0
	}
	bitpix := fitsInt(header["BITPIX"])
	if bitpix == 0 {
		return 0
	}
	abs := bitpix
	if abs < 0 {
		abs = -abs
	}
	bytesPerPixel := abs / 8
	if bytesPerPixel <= 0 {
		return 0
	}
	// Sanity-cap the axis count even though the header claims it —
	// FITS Standard §4.4.1 caps NAXIS at 999, but we won't trust an
	// adversarial header that far.
	if naxis > 999 {
		naxis = 999
	}
	product := int64(1)
	for i := int64(1); i <= naxis; i++ {
		ax := fitsInt(header["NAXIS"+strconv.FormatInt(i, 10)])
		if ax <= 0 {
			return 0
		}
		// Overflow guard: if the multiplication would wrap, return a
		// size that's guaranteed to overshoot the read window so the
		// walker terminates on the next iteration.
		if product > math.MaxInt64/ax {
			return math.MaxInt32
		}
		product *= ax
	}
	if product > math.MaxInt64/int64(bytesPerPixel) {
		return math.MaxInt32
	}
	size := product * int64(bytesPerPixel)
	// Round up to block boundary.
	rounded := (size + int64(fitsBlockSize) - 1) / int64(fitsBlockSize) * int64(fitsBlockSize)
	if rounded > int64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int(rounded)
}

// intValue coerces a header value to int64. Returns 0 for missing or
// non-integer values. FITS integer keywords parse as int64 in
// parseCard, so this mostly just type-asserts; the float fallback
// covers numeric values that parsed as float (rare for BITPIX/NAXIS
// but defensive against weird headers).
func fitsInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		if x > math.MaxInt64 || x < math.MinInt64 {
			return 0
		}
		return int64(x)
	}
	return 0
}

// floatValue coerces a header value to float64. Returns 0 for missing
// or non-numeric values.
func fitsFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	}
	return 0
}

// fitsString coerces a header value to string. Returns "" for
// missing or non-string values. Local helper renamed to avoid
// collision with the front-matter package-level stringValue.
func fitsString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// assembleFITSAttrs lifts the parsed primary header onto the
// CEL-visible attribute map. Promotes OBJECT → title, OBSERVER →
// author, DATE-OBS → taken_at to share vocabulary with the document /
// image families.
func assembleFITSAttrs(primary map[string]any, hduCount int) Attributes {
	out := scienceAttrs("fits", Attributes{
		"hdu_count": int64(hduCount),
		"fits_kind": fitsKind(primary),
	})

	// Per-format scalar attributes.
	if v := fitsString(primary["TELESCOP"]); v != "" {
		out["telescope"] = v
	}
	if v := fitsString(primary["INSTRUME"]); v != "" {
		out["instrument"] = v
	}
	if v := fitsString(primary["OBJECT"]); v != "" {
		out["object"] = v
		out["title"] = v
	}
	if v := fitsString(primary["OBSERVER"]); v != "" {
		out["observer"] = v
		out["author"] = v
	}
	if v := fitsString(primary["DATE-OBS"]); v != "" {
		out["date_obs"] = v
		if t, ok := parseFITSDate(v); ok {
			out["taken_at"] = t
		}
	}
	if v := fitsFloat(primary["EXPTIME"]); v != 0 {
		out["exptime"] = v
	}
	if v := fitsString(primary["FILTER"]); v != "" {
		out["filter"] = v
	}
	if v := fitsFloat(primary["AIRMASS"]); v != 0 {
		out["airmass"] = v
	}
	// CRVAL1/CRVAL2 are the WCS coordinate-reference-value pair (the
	// modern standard); RA/DEC are the older convention. Prefer WCS.
	if v := fitsFloat(primary["CRVAL1"]); v != 0 {
		out["ra"] = v
	} else if v := fitsFloat(primary["RA"]); v != 0 {
		out["ra"] = v
	}
	if v := fitsFloat(primary["CRVAL2"]); v != 0 {
		out["dec"] = v
	} else if v := fitsFloat(primary["DEC"]); v != 0 {
		out["dec"] = v
	}
	if v := fitsInt(primary["BITPIX"]); v != 0 {
		out["bitpix"] = v
	}
	// NAXIS / NAXISn are non-negative per FITS Standard §4.4.1 — adversarial
	// headers can claim e.g. `NAXIS1 = -1000000000000000000` and parse cleanly
	// as int64. Drop negatives at the surface so CEL consumers never see them.
	if v := fitsInt(primary["NAXIS"]); v > 0 {
		out["naxis"] = v
	}
	if v := fitsInt(primary["NAXIS1"]); v > 0 {
		out["naxis1"] = v
	}
	if v := fitsInt(primary["NAXIS2"]); v > 0 {
		out["naxis2"] = v
	}
	return out
}

// fitsKind classifies the primary HDU as image / table / binary-table
// / primary based on the header's SIMPLE / XTENSION + NAXIS keywords.
// Image-shaped primary HDUs (SIMPLE=T with NAXIS>0) classify as
// "image"; zero-axis primaries are "primary" (a metadata-only HDU,
// common when image data lives in extensions). XTENSION values come
// from FITS §4.1.2.2 — TABLE / BINTABLE / IMAGE — and map to the
// short canonical names used here.
func fitsKind(header map[string]any) string {
	if v, ok := header["XTENSION"].(string); ok && v != "" {
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "IMAGE":
			return "image"
		case "TABLE":
			return "table"
		case "BINTABLE":
			return "binary-table"
		}
		return strings.ToLower(strings.TrimSpace(v))
	}
	// Primary HDU. Distinguish image vs metadata-only via NAXIS.
	if fitsInt(header["NAXIS"]) > 0 {
		return "image"
	}
	return "primary"
}

// parseFITSDate parses the DATE-OBS keyword. FITS standardised on the
// `YYYY-MM-DDThh:mm:ss[.sss]` shape but older files use plain
// `YYYY-MM-DD`. We try the modern formats first.
func parseFITSDate(s string) (time.Time, bool) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// roundUp returns x rounded up to the nearest multiple of n. n must
// be positive; we don't bother guarding because all callers pass
// constant block sizes.
func roundUp(x, n int) int {
	return (x + n - 1) / n * n
}
