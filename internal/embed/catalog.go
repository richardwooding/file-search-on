package embed

// CatalogEntry describes one well-known embedding model that
// file-search-on knows about by name. The catalog is hand-curated and
// intentionally small — five entries covering the obvious choices.
// Locally-pulled models that aren't in the catalog still surface in
// list output; they're just not annotated with description / dims.
//
// Adding a new entry: append to Catalog in alphabetical order (the
// sanity test enforces ordering). Each entry needs Name (the bare
// `ollama pull` name, no tag), Description (one user-visible line),
// Size (human-readable, for display only), and Dimensions (the vector
// length the model emits — used to flag dimension mismatches before a
// dimension-validated query lands).
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        string `json:"size"`
	Dimensions  int    `json:"dimensions"`
}

// Catalog is the curated list of recommended embedding models. Sorted
// alphabetically by Name so list output is deterministic.
var Catalog = []CatalogEntry{
	{
		Name:        "all-minilm",
		Description: "Smallest. Great for memory-constrained machines; lower quality.",
		Size:        "~45 MB",
		Dimensions:  384,
	},
	{
		Name:        "bge-m3",
		Description: "Multilingual; supports dense + sparse + colbert outputs.",
		Size:        "~600 MB",
		Dimensions:  1024,
	},
	{
		Name:        "mxbai-embed-large",
		Description: "Higher dimensionality. Better recall on long-form prose; slower.",
		Size:        "~670 MB",
		Dimensions:  1024,
	},
	{
		Name:        "nomic-embed-text",
		Description: "Strong general-purpose. Good default for English documents.",
		Size:        "~270 MB",
		Dimensions:  768,
	},
	{
		Name:        "snowflake-arctic-embed",
		Description: "Snowflake's open embedding family. Solid for short technical text.",
		Size:        "~330 MB",
		Dimensions:  1024,
	},
}

// CatalogLookup returns the catalog entry for the given bare model name
// (without ":tag" suffix), or nil when the name isn't in the catalog.
// Used to annotate ListLocal results.
func CatalogLookup(bareName string) *CatalogEntry {
	for i := range Catalog {
		if Catalog[i].Name == bareName {
			return &Catalog[i]
		}
	}
	return nil
}

// BareName strips an Ollama tag suffix ("nomic-embed-text:latest" →
// "nomic-embed-text") so a tagged local model can match a catalog
// entry. Returns name unchanged when there's no colon.
func BareName(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == ':' {
			return name[:i]
		}
	}
	return name
}
