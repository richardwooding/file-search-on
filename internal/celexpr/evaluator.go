package celexpr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/richardwooding/file-search-on/internal/content"
)

// FileAttributes holds all attributes available in CEL expressions
type FileAttributes struct {
	Name        string
	Path        string
	Dir         string
	Size        int64
	Ext         string
	ModTime     time.Time
	ContentType string
	IsMarkdown  bool
	IsJSON      bool
	IsXML       bool
	IsHTML      bool
	IsPDF       bool
	IsImage     bool
	IsText      bool
	IsCSV       bool
	IsEPUB      bool
	IsOffice    bool
	IsAudio     bool
	Extra       content.Attributes
}

// Evaluator evaluates CEL expressions against file attributes
type Evaluator struct {
	env  *cel.Env
	prog cel.Program
}

// New creates a new evaluator for the given CEL expression
func New(expr string) (*Evaluator, error) {
	env, err := cel.NewEnv(
		cel.Variable("name", cel.StringType),
		cel.Variable("path", cel.StringType),
		cel.Variable("dir", cel.StringType),
		cel.Variable("size", cel.IntType),
		cel.Variable("ext", cel.StringType),
		cel.Variable("content_type", cel.StringType),
		cel.Variable("is_markdown", cel.BoolType),
		cel.Variable("is_json", cel.BoolType),
		cel.Variable("is_xml", cel.BoolType),
		cel.Variable("is_html", cel.BoolType),
		cel.Variable("is_pdf", cel.BoolType),
		cel.Variable("is_image", cel.BoolType),
		cel.Variable("is_text", cel.BoolType),
		cel.Variable("is_csv", cel.BoolType),
		cel.Variable("is_epub", cel.BoolType),
		cel.Variable("is_office", cel.BoolType),
		cel.Variable("is_audio", cel.BoolType),
		cel.Variable("title", cel.StringType),
		cel.Variable("word_count", cel.IntType),
		cel.Variable("line_count", cel.IntType),
		cel.Variable("column_count", cel.IntType),
		cel.Variable("csv_columns", cel.ListType(cel.StringType)),
		cel.Variable("language", cel.StringType),
		cel.Variable("page_count", cel.IntType),
		cel.Variable("author", cel.StringType),
		cel.Variable("root_element", cel.StringType),
		cel.Variable("json_kind", cel.StringType),
		cel.Variable("img_width", cel.IntType),
		cel.Variable("img_height", cel.IntType),
		cel.Variable("camera_make", cel.StringType),
		cel.Variable("camera_model", cel.StringType),
		cel.Variable("lens", cel.StringType),
		cel.Variable("taken_at", cel.TimestampType),
		cel.Variable("orientation", cel.IntType),
		cel.Variable("gps_lat", cel.DoubleType),
		cel.Variable("gps_lon", cel.DoubleType),
		cel.Variable("iso", cel.IntType),
		cel.Variable("focal_length", cel.DoubleType),
		cel.Variable("f_stop", cel.DoubleType),
		cel.Variable("exposure_time", cel.DoubleType),
		cel.Variable("artist", cel.StringType),
		cel.Variable("album", cel.StringType),
		cel.Variable("album_artist", cel.StringType),
		cel.Variable("composer", cel.StringType),
		cel.Variable("year", cel.IntType),
		cel.Variable("track", cel.IntType),
		cel.Variable("genre", cel.StringType),
		cel.Variable("frontmatter", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("frontmatter_format", cel.StringType),
		cel.Variable("tags", cel.ListType(cel.StringType)),
		cel.Variable("categories", cel.ListType(cel.StringType)),
		cel.Variable("draft", cel.BoolType),
		cel.Variable("date", cel.TimestampType),
	)
	if err != nil {
		return nil, fmt.Errorf("creating CEL environment: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling CEL expression: %w", issues.Err())
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating CEL program: %w", err)
	}

	return &Evaluator{env: env, prog: prog}, nil
}

// Evaluate evaluates the expression against the given file attributes
func (e *Evaluator) Evaluate(attrs *FileAttributes) (bool, error) {
	activation := map[string]any{
		"name":               attrs.Name,
		"path":               attrs.Path,
		"dir":                attrs.Dir,
		"size":               attrs.Size,
		"ext":                attrs.Ext,
		"content_type":       attrs.ContentType,
		"is_markdown":        attrs.IsMarkdown,
		"is_json":            attrs.IsJSON,
		"is_xml":             attrs.IsXML,
		"is_html":            attrs.IsHTML,
		"is_pdf":             attrs.IsPDF,
		"is_image":           attrs.IsImage,
		"is_text":            attrs.IsText,
		"is_csv":             attrs.IsCSV,
		"is_epub":            attrs.IsEPUB,
		"is_office":          attrs.IsOffice,
		"is_audio":           attrs.IsAudio,
		"title":              "",
		"word_count":         int64(0),
		"line_count":         int64(0),
		"column_count":       int64(0),
		"csv_columns":        []string{},
		"language":           "",
		"page_count":         int64(0),
		"author":             "",
		"root_element":       "",
		"json_kind":          "",
		"img_width":          int64(0),
		"img_height":         int64(0),
		"camera_make":        "",
		"camera_model":       "",
		"lens":               "",
		"taken_at":           time.Time{},
		"orientation":        int64(0),
		"gps_lat":            float64(0),
		"gps_lon":            float64(0),
		"iso":                int64(0),
		"focal_length":       float64(0),
		"f_stop":             float64(0),
		"exposure_time":      float64(0),
		"artist":             "",
		"album":              "",
		"album_artist":       "",
		"composer":           "",
		"year":               int64(0),
		"track":              int64(0),
		"genre":              "",
		"frontmatter":        map[string]any{},
		"frontmatter_format": "",
		"tags":               []string{},
		"categories":         []string{},
		"draft":              false,
		"date":               time.Time{},
	}

	if attrs.Extra != nil {
		for k, v := range attrs.Extra {
			switch k {
			case "title":
				activation["title"] = v
			case "word_count":
				activation["word_count"] = v
			case "line_count":
				activation["line_count"] = v
			case "column_count":
				activation["column_count"] = v
			case "csv_columns":
				activation["csv_columns"] = v
			case "language":
				activation["language"] = v
			case "page_count":
				activation["page_count"] = v
			case "author":
				activation["author"] = v
			case "root_element":
				activation["root_element"] = v
			case "kind":
				activation["json_kind"] = v
			case "width":
				activation["img_width"] = v
			case "height":
				activation["img_height"] = v
			case "camera_make":
				activation["camera_make"] = v
			case "camera_model":
				activation["camera_model"] = v
			case "lens":
				activation["lens"] = v
			case "taken_at":
				activation["taken_at"] = v
			case "orientation":
				activation["orientation"] = v
			case "gps_lat":
				activation["gps_lat"] = v
			case "gps_lon":
				activation["gps_lon"] = v
			case "iso":
				activation["iso"] = v
			case "focal_length":
				activation["focal_length"] = v
			case "f_stop":
				activation["f_stop"] = v
			case "exposure_time":
				activation["exposure_time"] = v
			case "artist":
				activation["artist"] = v
			case "album":
				activation["album"] = v
			case "album_artist":
				activation["album_artist"] = v
			case "composer":
				activation["composer"] = v
			case "year":
				activation["year"] = v
			case "track":
				activation["track"] = v
			case "genre":
				activation["genre"] = v
			case "frontmatter":
				activation["frontmatter"] = v
			case "frontmatter_format":
				activation["frontmatter_format"] = v
			case "tags":
				activation["tags"] = v
			case "categories":
				activation["categories"] = v
			case "draft":
				activation["draft"] = v
			case "date":
				activation["date"] = v
			}
		}
	}

	out, _, err := e.prog.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("evaluating CEL expression: %w", err)
	}

	return out == types.True, nil
}

// BuildAttributes builds file attributes for a given path. ctx is checked
// at entry and threaded into ContentType.Attributes so per-file work can be
// cancelled mid-scan.
func BuildAttributes(ctx context.Context, path string, registry *content.Registry) (*FileAttributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	name := info.Name()
	ext := strings.ToLower(filepath.Ext(name))
	dir := filepath.Dir(path)

	ct := registry.Detect(path)
	contentTypeName := ""
	isMarkdown, isJSON, isXML, isHTML, isPDF, isImage := false, false, false, false, false, false
	isText, isCSV, isEPUB, isOffice, isAudio := false, false, false, false, false

	var extra content.Attributes
	if ct != nil {
		contentTypeName = ct.Name()
		switch {
		case contentTypeName == "markdown":
			isMarkdown = true
		case contentTypeName == "json":
			isJSON = true
		case contentTypeName == "xml":
			isXML = true
		case contentTypeName == "html":
			isHTML = true
		case contentTypeName == "pdf":
			isPDF = true
		case contentTypeName == "text":
			isText = true
		case contentTypeName == "csv":
			isCSV = true
		case contentTypeName == "epub":
			isEPUB = true
		case strings.HasPrefix(contentTypeName, "image/"):
			isImage = true
		case strings.HasPrefix(contentTypeName, "office/"):
			isOffice = true
		case strings.HasPrefix(contentTypeName, "audio/"):
			isAudio = true
		}
		extra, err = ct.Attributes(ctx, path)
		if err != nil {
			return nil, err
		}
	}

	return &FileAttributes{
		Name:        name,
		Path:        path,
		Dir:         dir,
		Size:        info.Size(),
		Ext:         ext,
		ModTime:     info.ModTime(),
		ContentType: contentTypeName,
		IsMarkdown:  isMarkdown,
		IsJSON:      isJSON,
		IsXML:       isXML,
		IsHTML:      isHTML,
		IsPDF:       isPDF,
		IsImage:     isImage,
		IsText:      isText,
		IsCSV:       isCSV,
		IsEPUB:      isEPUB,
		IsOffice:    isOffice,
		IsAudio:     isAudio,
		Extra:       extra,
	}, nil
}
