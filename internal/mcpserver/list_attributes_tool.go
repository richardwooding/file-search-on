package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// ContentTypeDoc describes a registered content type.
type ContentTypeDoc struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// ListAttributesInput selects which slice of the schema list_attributes
// returns. All fields optional — the default (no args) returns a small
// summary that the agent can drill into. Issue #273.
//
// Precedence (first non-empty wins):
//
//  1. names — exact-name lookup across every section. Returns the
//     matching attribute / function / content-type entries from
//     wherever they live. Use when an agent already knows what it's
//     looking up.
//  2. section — fetch one full section (common / type_specific /
//     frontmatter / functions / content_types) with optional
//     limit + offset pagination. The full type_specific section is
//     ~250 entries / >80kB JSON; paginate when total > limit.
//  3. neither — summary mode: counts per section + every function's
//     name + a hint string explaining how to drill in. Sub-1kB
//     response — token-safe for MCP framing.
type ListAttributesInput struct {
	Section string   `json:"section,omitempty" jsonschema:"Fetch one full section: 'common' (attrs on every file), 'type_specific' (per-content-type attrs), 'frontmatter' (markdown YAML keys), 'functions' (CEL builtins like levenshtein/soundex), 'content_types' (registered content type names + extensions). Empty returns a summary instead. Unknown values error."`
	Names   []string `json:"names,omitempty" jsonschema:"Direct name lookup across every section. Returns matching attribute / function / content-type entries from wherever they live. Use when you already know what you're looking up (saves an extra summary round-trip)."`
	Limit   int      `json:"limit,omitempty" jsonschema:"Cap the per-section response. Default 50; server cap 500. Ignored in names + summary modes."`
	Offset  int      `json:"offset,omitempty" jsonschema:"Pagination offset within a section. Ignored in names + summary modes."`
}

// ListAttributesOutput is one of three shapes depending on Mode:
//
//   - Mode=summary  → Summary populated; everything else empty
//   - Mode=section  → Section + (Attributes | Functions | ContentTypes)
//     populated; Total / Limit / Offset describe the slice
//   - Mode=names    → Attributes / Functions / ContentTypes hold every
//     match found in any section; Total counts the union
type ListAttributesOutput struct {
	CommonOutput
	Mode         string                 `json:"mode"`
	Summary      *SchemaSummary         `json:"summary,omitempty"`
	Section      string                 `json:"section,omitempty"`
	Attributes   []celexpr.AttributeDoc `json:"attributes,omitempty"`
	Functions    []celexpr.FunctionDoc  `json:"functions,omitempty"`
	ContentTypes []ContentTypeDoc       `json:"content_types,omitempty"`
	Total        int                    `json:"total,omitempty"`
	Limit        int                    `json:"limit,omitempty"`
	Offset       int                    `json:"offset,omitempty"`
}

// SchemaSummary is the default (no-args) response: counts per section
// plus the cheap-to-enumerate function-name list and a hint. Together
// this is small enough to land in any MCP client's response budget.
type SchemaSummary struct {
	Sections      map[string]int `json:"sections"`
	FunctionNames []string       `json:"function_names"`
	Hint          string         `json:"hint"`
}

// listAttributesDefaultLimit / Cap mirror cache-entries pagination so
// agents can reuse the same mental model.
const (
	listAttributesDefaultLimit = 50
	listAttributesLimitCap     = 500
)

func (h *handlers) listAttributesHandler(_ context.Context, _ *mcp.CallToolRequest, in ListAttributesInput) (*mcp.CallToolResult, ListAttributesOutput, error) {
	schema := celexpr.Schema()
	contentTypes := contentTypeDocs()

	out := ListAttributesOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
	}

	// Names mode: lookup across every section.
	if len(in.Names) > 0 {
		out.Mode = "names"
		want := make(map[string]struct{}, len(in.Names))
		for _, n := range in.Names {
			want[n] = struct{}{}
		}
		for _, a := range schema.Common {
			if _, ok := want[a.Name]; ok {
				out.Attributes = append(out.Attributes, a)
			}
		}
		for _, a := range schema.TypeSpecific {
			if _, ok := want[a.Name]; ok {
				out.Attributes = append(out.Attributes, a)
			}
		}
		for _, a := range schema.Frontmatter {
			if _, ok := want[a.Name]; ok {
				out.Attributes = append(out.Attributes, a)
			}
		}
		for _, f := range schema.Functions {
			if _, ok := want[f.Name]; ok {
				out.Functions = append(out.Functions, f)
			}
		}
		for _, ct := range contentTypes {
			if _, ok := want[ct.Name]; ok {
				out.ContentTypes = append(out.ContentTypes, ct)
			}
		}
		out.Total = len(out.Attributes) + len(out.Functions) + len(out.ContentTypes)
		return nil, out, nil
	}

	// Section mode: paginate the requested slice.
	if in.Section != "" {
		out.Mode = "section"
		out.Section = in.Section
		limit := in.Limit
		if limit <= 0 {
			limit = listAttributesDefaultLimit
		}
		if limit > listAttributesLimitCap {
			limit = listAttributesLimitCap
		}
		offset := max(in.Offset, 0)
		out.Limit = limit
		out.Offset = offset

		switch in.Section {
		case "common":
			out.Total = len(schema.Common)
			out.Attributes = paginateAttrs(schema.Common, offset, limit)
		case "type_specific":
			out.Total = len(schema.TypeSpecific)
			out.Attributes = paginateAttrs(schema.TypeSpecific, offset, limit)
		case "frontmatter":
			out.Total = len(schema.Frontmatter)
			out.Attributes = paginateAttrs(schema.Frontmatter, offset, limit)
		case "functions":
			out.Total = len(schema.Functions)
			out.Functions = paginateFuncs(schema.Functions, offset, limit)
		case "content_types":
			out.Total = len(contentTypes)
			out.ContentTypes = paginateCTs(contentTypes, offset, limit)
		default:
			return nil, ListAttributesOutput{}, fmt.Errorf(
				"unknown section %q (want one of: common, type_specific, frontmatter, functions, content_types)",
				in.Section,
			)
		}
		return nil, out, nil
	}

	// Default: summary.
	out.Mode = "summary"
	fnNames := make([]string, len(schema.Functions))
	for i, f := range schema.Functions {
		fnNames[i] = f.Name
	}
	out.Summary = &SchemaSummary{
		Sections: map[string]int{
			"common":        len(schema.Common),
			"type_specific": len(schema.TypeSpecific),
			"frontmatter":   len(schema.Frontmatter),
			"functions":     len(schema.Functions),
			"content_types": len(contentTypes),
		},
		FunctionNames: fnNames,
		Hint: strings.Join([]string{
			"Pass section: 'common' for every-file attrs (~35),",
			"'type_specific' for per-content-type attrs (~250; paginate with limit+offset),",
			"'frontmatter' for markdown YAML keys (~12),",
			"'functions' for CEL builtins (~8),",
			"'content_types' for registered content type names (~100).",
			"Pass names: ['loc', 'symbols', 'levenshtein', ...] to fetch specific entries across every section in one call.",
		}, " "),
	}
	return nil, out, nil
}

// contentTypeDocs builds the registry view used by both section=content_types
// and names mode. Extracted so both code paths share one source of truth.
func contentTypeDocs() []ContentTypeDoc {
	types := content.DefaultRegistry().Types()
	docs := make([]ContentTypeDoc, len(types))
	for i, t := range types {
		docs[i] = ContentTypeDoc{Name: t.Name(), Extensions: t.Extensions()}
	}
	return docs
}

// paginateAttrs returns the [offset, offset+limit) slice of attrs, with
// offset clamped to len(attrs) so over-runs return an empty slice
// rather than panicking. Mirrors the index/list.go ListAttrs shape.
func paginateAttrs(attrs []celexpr.AttributeDoc, offset, limit int) []celexpr.AttributeDoc {
	if offset >= len(attrs) {
		return nil
	}
	end := min(offset+limit, len(attrs))
	out := make([]celexpr.AttributeDoc, end-offset)
	copy(out, attrs[offset:end])
	return out
}

func paginateFuncs(fns []celexpr.FunctionDoc, offset, limit int) []celexpr.FunctionDoc {
	if offset >= len(fns) {
		return nil
	}
	end := min(offset+limit, len(fns))
	out := make([]celexpr.FunctionDoc, end-offset)
	copy(out, fns[offset:end])
	return out
}

func paginateCTs(cts []ContentTypeDoc, offset, limit int) []ContentTypeDoc {
	if offset >= len(cts) {
		return nil
	}
	end := min(offset+limit, len(cts))
	out := make([]ContentTypeDoc, end-offset)
	copy(out, cts[offset:end])
	return out
}
