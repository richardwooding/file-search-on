package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/richardwooding/ollamaembed"
)

// ListEmbeddingModelsInput is the input shape for `list_embedding_models`.
type ListEmbeddingModelsInput struct {
	// EmbeddingServer overrides the server's default Ollama base URL for
	// this call. Useful when querying a remote Ollama (e.g. http://gpu-box:11434).
	EmbeddingServer string `json:"embedding_server,omitempty" jsonschema:"Override the server's default Ollama base URL for this call. Defaults to the server-startup setting (--embedding-server / OLLAMA_HOST) or http://localhost:11434."`
}

// ListEmbeddingModelsOutput pairs the Ollama-local list with the
// file-search-on curated catalog so agents can answer both 'what's
// installed?' and 'what could I install?' in one call.
type ListEmbeddingModelsOutput struct {
	CommonOutput
	// Server is the resolved Ollama base URL the response describes.
	Server string `json:"server"`
	// Local is every model present on the Ollama server (chat AND
	// embedding mixed — Ollama doesn't classify and neither do we).
	// Each entry's Catalogued flag is true when its bare name matches
	// the curated catalog; if so the entry also carries Description
	// and Dimensions copied from the catalog.
	Local []LocalModelOut `json:"local"`
	// Catalog is the curated list of recommended embedding models, with
	// each entry marked Pulled=true if any local model has a matching
	// bare name. Agents pulling a fresh model pick from here.
	Catalog []CatalogOut `json:"catalog"`
}

// LocalModelOut is a single row of ListEmbeddingModelsOutput.Local.
type LocalModelOut struct {
	Name        string    `json:"name"`
	SizeBytes   int64     `json:"size_bytes"`
	ModifiedAt  time.Time `json:"modified_at"`
	Digest      string    `json:"digest"`
	Catalogued  bool      `json:"catalogued"`
	Description string    `json:"description,omitempty"`
	Dimensions  int       `json:"dimensions,omitempty"`
}

// CatalogOut is a single row of ListEmbeddingModelsOutput.Catalog.
type CatalogOut struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        string `json:"size"`
	Dimensions  int    `json:"dimensions"`
	Pulled      bool   `json:"pulled"`
}

func (h *handlers) listEmbeddingModelsHandler(ctx context.Context, _ *mcp.CallToolRequest, in ListEmbeddingModelsInput) (*mcp.CallToolResult, ListEmbeddingModelsOutput, error) {
	server := in.EmbeddingServer
	if server == "" {
		server = h.defaultEmbeddingServer
	}

	out := ListEmbeddingModelsOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Server:       server,
		Local:        []LocalModelOut{},
		Catalog:      make([]CatalogOut, 0, len(ollamaembed.Catalog)),
	}

	oll := ollamaembed.NewOllama(server, "")
	local, err := oll.ListLocal(ctx)
	if err != nil {
		return nil, out, err
	}

	pulledBare := make(map[string]struct{}, len(local))
	for _, m := range local {
		bare := ollamaembed.BareName(m.Name)
		pulledBare[bare] = struct{}{}
		row := LocalModelOut{
			Name:       m.Name,
			SizeBytes:  m.Size,
			ModifiedAt: m.ModifiedAt,
			Digest:     m.Digest,
		}
		if cat := ollamaembed.CatalogLookup(bare); cat != nil {
			row.Catalogued = true
			row.Description = cat.Description
			row.Dimensions = cat.Dimensions
		}
		out.Local = append(out.Local, row)
	}

	for _, c := range ollamaembed.Catalog {
		_, pulled := pulledBare[c.Name]
		out.Catalog = append(out.Catalog, CatalogOut{
			Name:        c.Name,
			Description: c.Description,
			Size:        c.Size,
			Dimensions:  c.Dimensions,
			Pulled:      pulled,
		})
	}

	return nil, out, nil
}
