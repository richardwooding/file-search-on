package search_test

import (
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestMatchFrom_ScienceAttrs covers applyScienceAttrs (#510): the science-data
// family projection was 0% covered. It feeds a FITS Result and asserts the
// projected Match carries the science fields (string / int64 / []string) and
// the typed family predicates.
func TestMatchFrom_ScienceAttrs(t *testing.T) {
	r := search.Result{
		Path:        "/data/obs.fits",
		ContentType: "science/fits",
		Size:        4096,
		Attrs: &celexpr.FileAttributes{
			IsFITS:        true,
			IsScienceData: true,
			Extra: map[string]any{
				"science_format": "FITS",
				"telescope":      "HST",
				"instrument":     "WFC3",
				"object":         "M31",
				"naxis":          int64(2),
				"naxis1":         int64(1024),
				"hdu_count":      int64(3),
				"fits_kind":      "image",
				// VOTable fields share the helper.
				"votable_version": "1.4",
				"table_count":     int64(2),
				"total_rows":      int64(500),
				"field_names":     []string{"ra", "dec", "mag"},
			},
		},
	}
	m := search.MatchFrom(r)

	if !m.IsFITS || !m.IsScienceData {
		t.Errorf("typed science predicates not copied: IsFITS=%v IsScienceData=%v", m.IsFITS, m.IsScienceData)
	}
	if m.ScienceFormat != "FITS" || m.Telescope != "HST" || m.Instrument != "WFC3" || m.Object != "M31" {
		t.Errorf("science string fields wrong: %+v", m)
	}
	if m.Naxis != 2 || m.Naxis1 != 1024 || m.HDUCount != 3 {
		t.Errorf("science int fields wrong: Naxis=%d Naxis1=%d HDUCount=%d", m.Naxis, m.Naxis1, m.HDUCount)
	}
	if m.FITSKind != "image" {
		t.Errorf("FITSKind = %q, want image", m.FITSKind)
	}
	if m.VOTableVersion != "1.4" || m.TableCount != 2 || m.TotalRows != 500 {
		t.Errorf("votable fields wrong: %+v", m)
	}
	if len(m.FieldNames) != 3 || m.FieldNames[0] != "ra" {
		t.Errorf("FieldNames = %v, want [ra dec mag]", m.FieldNames)
	}
}

// TestMatchFrom_FontAttrs covers applyFontAttrs (#510): the font family
// projection was 0% covered. Asserts string / int64 / []string / bool font
// attributes round-trip onto Match.
func TestMatchFrom_FontAttrs(t *testing.T) {
	r := search.Result{
		Path:        "/fonts/Inter.ttf",
		ContentType: "font/ttf",
		Size:        2048,
		Attrs: &celexpr.FileAttributes{
			IsFont: true,
			IsTTF:  true,
			Extra: map[string]any{
				"is_variable_font":    true,
				"is_monospace_font":   false,
				"font_format":         "TrueType",
				"font_family":         "Inter",
				"font_subfamily":      "Regular",
				"font_units_per_em":   int64(2048),
				"font_glyph_count":    int64(2548),
				"font_axis_count":     int64(2),
				"font_unicode_ranges": []string{"Basic Latin", "Latin-1 Supplement"},
			},
		},
	}
	m := search.MatchFrom(r)

	if !m.IsFont || !m.IsTTF {
		t.Errorf("typed font predicates not copied: IsFont=%v IsTTF=%v", m.IsFont, m.IsTTF)
	}
	if !m.IsVariableFont {
		t.Errorf("IsVariableFont should be true")
	}
	if m.FontFormat != "TrueType" || m.FontFamily != "Inter" || m.FontSubfamily != "Regular" {
		t.Errorf("font string fields wrong: %+v", m)
	}
	if m.FontUnitsPerEm != 2048 || m.FontGlyphCount != 2548 || m.FontAxisCount != 2 {
		t.Errorf("font int fields wrong: UnitsPerEm=%d GlyphCount=%d AxisCount=%d", m.FontUnitsPerEm, m.FontGlyphCount, m.FontAxisCount)
	}
	if len(m.FontUnicodeRanges) != 2 {
		t.Errorf("FontUnicodeRanges = %v, want 2 entries", m.FontUnicodeRanges)
	}
}

// TestMatchFrom_ComputedAndPredicates covers applyComputedAttrs and
// applyTypedPredicates (#510). Computed attrs (phash / similarity / timestamps
// / git metadata) and the typed is_* predicates were 0% covered.
func TestMatchFrom_ComputedAndPredicates(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	commit := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	r := search.Result{
		Path:        "/x.md",
		ContentType: "markdown",
		Size:        10,
		Attrs: &celexpr.FileAttributes{
			IsMarkdown:          true,
			IsImage:             true, // exercise more predicate copies
			Similarity:          0.87,
			MatchStartLine:      12,
			CreatedAt:           created,
			GitLastCommitTime:   commit,
			GitLastCommitAuthor: "Alice",
			GitCommitCount:      7,
			IsGitTracked:        true,
			Extra: map[string]any{
				"phash": "ffaa0011",
			},
		},
	}
	m := search.MatchFrom(r)

	if !m.IsMarkdown || !m.IsImage {
		t.Errorf("typed predicates not copied: IsMarkdown=%v IsImage=%v", m.IsMarkdown, m.IsImage)
	}
	if m.PHash != "ffaa0011" {
		t.Errorf("PHash = %q, want ffaa0011", m.PHash)
	}
	if m.Similarity != 0.87 || m.MatchStartLine != 12 {
		t.Errorf("computed match fields wrong: Similarity=%v MatchStartLine=%d", m.Similarity, m.MatchStartLine)
	}
	if m.CreatedAt != created.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", m.CreatedAt, created.Format(time.RFC3339))
	}
	if m.GitLastCommitTime != commit.Format(time.RFC3339) {
		t.Errorf("GitLastCommitTime = %q, want %q", m.GitLastCommitTime, commit.Format(time.RFC3339))
	}
	if m.GitLastCommitAuthor != "Alice" || m.GitCommitCount != 7 || !m.IsGitTracked {
		t.Errorf("git fields wrong: %+v", m)
	}
}
