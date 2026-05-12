package celexpr

import (
	"time"

	"github.com/google/cel-go/cel"
)

// fileAttrsActivation is a cel.Activation backed directly by
// *FileAttributes — no per-Evaluate map allocation. Replaces the
// 100-entry map literal that dominated walker allocations after the
// markdown scanner was right-sized (#90 + #91 profiles flagged
// Evaluate as the next-biggest contributor at ~35%).
//
// Struct fields short-circuit first; Extra-driven attributes come
// next; declared variables that the matched content-type family
// didn't populate fall back to a static zero default.
//
// Sharing the zero defaults map across calls is safe: cel-go treats
// activation inputs as read-only, so the immutable empty slice /
// empty map sentinels never get mutated through it.
type fileAttrsActivation struct {
	attrs *FileAttributes
}

// ResolveName returns the CEL-visible value for the named variable.
// Returns false only for names that aren't declared in the env —
// which shouldn't happen since the env declares the full set up
// front and the CEL compiler rejects expressions referencing
// undeclared vars.
func (a *fileAttrsActivation) ResolveName(name string) (any, bool) {
	switch name {
	case "name":
		return a.attrs.Name, true
	case "path":
		return a.attrs.Path, true
	case "dir":
		return a.attrs.Dir, true
	case "size":
		return a.attrs.Size, true
	case "ext":
		return a.attrs.Ext, true
	case "content_type":
		return a.attrs.ContentType, true
	case "is_markdown":
		return a.attrs.IsMarkdown, true
	case "is_json":
		return a.attrs.IsJSON, true
	case "is_xml":
		return a.attrs.IsXML, true
	case "is_html":
		return a.attrs.IsHTML, true
	case "is_pdf":
		return a.attrs.IsPDF, true
	case "is_image":
		return a.attrs.IsImage, true
	case "is_text":
		return a.attrs.IsText, true
	case "is_csv":
		return a.attrs.IsCSV, true
	case "is_epub":
		return a.attrs.IsEPUB, true
	case "is_office":
		return a.attrs.IsOffice, true
	case "is_audio":
		return a.attrs.IsAudio, true
	case "is_video":
		return a.attrs.IsVideo, true
	case "is_archive":
		return a.attrs.IsArchive, true
	case "is_binary":
		return a.attrs.IsBinary, true
	case "is_email":
		return a.attrs.IsEmail, true
	case "is_source":
		return a.attrs.IsSource, true
	case "is_notebook":
		return a.attrs.IsNotebook, true
	case "is_yaml":
		return a.attrs.IsYAML, true
	}
	if v, ok := a.attrs.Extra[name]; ok {
		return v, true
	}
	if v, ok := zeroDefaults[name]; ok {
		return v, true
	}
	return nil, false
}

// Parent returns nil — fileAttrsActivation is a leaf binding, the
// only activation cel-go sees per Evaluate call.
func (a *fileAttrsActivation) Parent() cel.Activation {
	return nil
}

// zeroDefaults holds the type-appropriate zero value for every
// declared CEL variable that's populated through FileAttributes.Extra.
// Built once at package init and never mutated.
var zeroDefaults = map[string]any{
	"title":                 "",
	"body":                  "",
	"word_count":            int64(0),
	"line_count":            int64(0),
	"column_count":          int64(0),
	"csv_columns":           []string{},
	"language":              "",
	"page_count":            int64(0),
	"author":                "",
	"root_element":          "",
	"json_kind":             "",
	"yaml_kind":             "",
	"yaml_document_count":   int64(0),
	"img_width":             int64(0),
	"img_height":            int64(0),
	"camera_make":           "",
	"camera_model":          "",
	"lens":                  "",
	"taken_at":              time.Time{},
	"orientation":           int64(0),
	"gps_lat":               float64(0),
	"gps_lon":               float64(0),
	"iso":                   int64(0),
	"focal_length":          float64(0),
	"f_stop":                float64(0),
	"exposure_time":         float64(0),
	"artist":                "",
	"album":                 "",
	"album_artist":          "",
	"composer":              "",
	"year":                  int64(0),
	"track":                 int64(0),
	"genre":                 "",
	"duration":              float64(0),
	"bitrate":               int64(0),
	"sample_rate":           int64(0),
	"channels":              int64(0),
	"bit_depth":             int64(0),
	"video_codec":           "",
	"audio_codec":           "",
	"video_width":           int64(0),
	"video_height":          int64(0),
	"frame_rate":            float64(0),
	"rotation":              int64(0),
	"nominal_bitrate":       int64(0),
	"color_primaries":       "",
	"color_transfer":        "",
	"is_hdr":                false,
	"subtitles":             false,
	"subtitle_languages":    []string{},
	"replaygain_track_gain": float64(0),
	"replaygain_album_gain": float64(0),
	"entry_count":           int64(0),
	"uncompressed_size":     int64(0),
	"top_level_entries":     []string{},
	"has_root_dir":          false,
	"architectures":         []string{},
	"bitness":               int64(0),
	"binary_format":         "",
	"binary_type":           "",
	"is_dynamically_linked": false,
	"is_stripped":           false,
	"entry_point":           int64(0),
	"email_to":              []string{},
	"email_cc":              []string{},
	"email_message_id":      "",
	"email_in_reply_to":     "",
	"sent_at":               time.Time{},
	"attachment_count":      int64(0),
	"email_count":           int64(0),
	"loc":                   int64(0),
	"comment_loc":           int64(0),
	"blank_loc":             int64(0),
	"cell_count":            int64(0),
	"code_cell_count":       int64(0),
	"markdown_cell_count":   int64(0),
	"kernel":                "",
	"frontmatter":           map[string]any{},
	"frontmatter_format":    "",
	"tags":                  []string{},
	"categories":            []string{},
	"draft":                 false,
	"date":                  time.Time{},
}
