package content

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
)

// notebook content types cover the two prevailing JSON-on-disk
// notebook formats:
//
//   - Jupyter (.ipynb): a top-level object with "cells" (array of
//     {cell_type, source, ...}), "metadata.kernelspec" (kernel
//     name + language), and "nbformat" (version).
//   - Zeppelin (.zpln): a top-level object with "paragraphs"
//     (array of {text, config.editorSetting.language, ...}),
//     "name" (notebook title), and "defaultInterpreterGroup".
//
// Both expose a shared set of attributes — cell_count,
// code_cell_count, markdown_cell_count, kernel, language, title —
// so agents can write content-type-agnostic CEL filters like
// `is_notebook && cell_count > 20`.
//
// Detection is extension-only. The files are JSON, but `.json` is
// also registered; the detector's longest-suffix match ensures
// `.ipynb` and `.zpln` win over `.json` for those specific exts.

func init() {
	Register(&jupyterType{})
	Register(&zeppelinType{})
}

type jupyterType struct{}

func (j *jupyterType) Name() string         { return "notebook/jupyter" }
func (j *jupyterType) Extensions() []string { return []string{".ipynb"} }
func (j *jupyterType) MagicBytes() [][]byte { return nil }

// jupyterDoc mirrors the subset of the .ipynb schema we care about
// (the official schema at jupyter.org has many more fields; we
// ignore them).
type jupyterDoc struct {
	NBFormat      int64               `json:"nbformat"`
	NBFormatMinor int64               `json:"nbformat_minor"`
	Cells         []jupyterCell       `json:"cells"`
	Metadata      jupyterMeta         `json:"metadata"`
	Worksheets    []jupyterWorksheet  `json:"worksheets"` // pre-v4 notebooks
}

type jupyterCell struct {
	CellType string `json:"cell_type"` // "code" | "markdown" | "raw"
}

type jupyterMeta struct {
	KernelSpec   jupyterKernelSpec   `json:"kernelspec"`
	LanguageInfo jupyterLanguageInfo `json:"language_info"`
}

type jupyterKernelSpec struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Language    string `json:"language"`
}

type jupyterLanguageInfo struct {
	Name string `json:"name"`
}

type jupyterWorksheet struct {
	Cells []jupyterCell `json:"cells"`
}

func (j *jupyterType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Cap the read at MaxLineBytes equivalent so an enormous notebook
	// (hundreds of MB of base64-encoded output) doesn't dominate the
	// walker. The structural fields we care about all live near the
	// top; once we've parsed them we're done. But json.Decoder needs
	// the whole document, so use a LimitReader as a soft upper bound.
	data, err := io.ReadAll(io.LimitReader(f, int64(MaxLineBytes())*8))
	if err != nil {
		return nil, err
	}

	var doc jupyterDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		// Malformed notebook → return empty attributes rather than
		// failing the walk. Matches how other parsers degrade.
		return Attributes{}, nil
	}

	// Modern (v4+) notebooks put cells at the top level; pre-v4
	// nested them inside worksheets. Sum both for robustness.
	cells := doc.Cells
	for _, ws := range doc.Worksheets {
		cells = append(cells, ws.Cells...)
	}

	var code, markdown int64
	for _, c := range cells {
		switch c.CellType {
		case "code":
			code++
		case "markdown":
			markdown++
		}
	}

	// Kernel: prefer the kernelspec name (machine-readable), fall
	// back to display_name. Language: prefer language_info.name (the
	// canonical lower-case form like "python", "r"), fall back to
	// kernelspec.language.
	kernel := doc.Metadata.KernelSpec.Name
	if kernel == "" {
		kernel = doc.Metadata.KernelSpec.DisplayName
	}
	language := doc.Metadata.LanguageInfo.Name
	if language == "" {
		language = doc.Metadata.KernelSpec.Language
	}

	attrs := Attributes{
		"cell_count":          int64(len(cells)),
		"code_cell_count":     code,
		"markdown_cell_count": markdown,
	}
	if kernel != "" {
		attrs["kernel"] = kernel
	}
	if language != "" {
		attrs["language"] = language
	}
	return attrs, nil
}

type zeppelinType struct{}

func (z *zeppelinType) Name() string         { return "notebook/zeppelin" }
func (z *zeppelinType) Extensions() []string { return []string{".zpln"} }
func (z *zeppelinType) MagicBytes() [][]byte { return nil }

// zeppelinDoc mirrors the subset of Zeppelin's note JSON we care
// about. Zeppelin paragraphs each carry their own interpreter prefix
// (e.g. "%spark", "%md") in the paragraph text — we don't try to
// parse that into a per-cell language, but the notebook-level
// defaultInterpreterGroup gives a useful "kernel" analogue.
type zeppelinDoc struct {
	Name                    string              `json:"name"`
	DefaultInterpreterGroup string              `json:"defaultInterpreterGroup"`
	Paragraphs              []zeppelinParagraph `json:"paragraphs"`
}

type zeppelinParagraph struct {
	Text   string                 `json:"text"`
	Config zeppelinParagraphConfig `json:"config"`
}

type zeppelinParagraphConfig struct {
	EditorSetting zeppelinEditorSetting `json:"editorSetting"`
}

type zeppelinEditorSetting struct {
	Language string `json:"language"`
}

func (z *zeppelinType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, int64(MaxLineBytes())*8))
	if err != nil {
		return nil, err
	}

	var doc zeppelinDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return Attributes{}, nil
	}

	// Zeppelin paragraphs don't carry a "cell_type" the way Jupyter
	// does. The closest analogue: the editor language set on the
	// paragraph, or the leading "%interpreter" line of paragraph
	// text. We count "%md" paragraphs as markdown for the sake of
	// cross-format parity with Jupyter; everything else is "code".
	var code, markdown int64
	for _, par := range doc.Paragraphs {
		if isZeppelinMarkdown(par) {
			markdown++
		} else {
			code++
		}
	}

	attrs := Attributes{
		"cell_count":          int64(len(doc.Paragraphs)),
		"code_cell_count":     code,
		"markdown_cell_count": markdown,
	}
	if doc.Name != "" {
		attrs["title"] = doc.Name
	}
	if doc.DefaultInterpreterGroup != "" {
		attrs["kernel"] = doc.DefaultInterpreterGroup
	}
	return attrs, nil
}

// isZeppelinMarkdown classifies a paragraph as markdown if either
// the editor setting says so OR the paragraph text begins with
// "%md".
func isZeppelinMarkdown(par zeppelinParagraph) bool {
	if par.Config.EditorSetting.Language == "markdown" || par.Config.EditorSetting.Language == "md" {
		return true
	}
	t := par.Text
	for len(t) > 0 && (t[0] == ' ' || t[0] == '\t' || t[0] == '\n' || t[0] == '\r') {
		t = t[1:]
	}
	return len(t) >= 3 && t[0] == '%' && (t[1] == 'm' || t[1] == 'M') && (t[2] == 'd' || t[2] == 'D')
}
