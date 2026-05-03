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
	isText, isCSV, isEPUB, isOffice := false, false, false, false

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
		Extra:       extra,
	}, nil
}
