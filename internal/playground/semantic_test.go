package playground

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

// fakeEmbedder is a deterministic, network-free Embedder for tests.
type fakeEmbedder struct{ model string }

func (f fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (f fakeEmbedder) Model() string { return f.model }

// semRes builds a result carrying a similarity score.
func semRes(path string, sim float64, md bool) search.Result {
	return search.Result{Path: path, Attrs: &celexpr.FileAttributes{
		Path: path, Name: path, ContentType: "markdown", Size: 100, IsMarkdown: md, Similarity: sim,
	}}
}

func semanticModel() model {
	return newModel(context.Background(), RunOptions{
		Embedder:            fakeEmbedder{model: "all-minilm"},
		EmbeddingModel:      "all-minilm",
		EmbeddingServer:     defaultServer,
		SimilarityThreshold: 0.5,
	})
}

func TestSemantic_ModeEnabledByEmbedder(t *testing.T) {
	plain := newModel(context.Background(), RunOptions{})
	if plain.semantic {
		t.Fatal("no embedder → semantic mode must be off")
	}
	m := semanticModel()
	if !m.semantic {
		t.Fatal("an embedder should enable semantic mode")
	}
	if m.focus != focusSem {
		t.Errorf("semantic mode should start focused on the query box, got %v", m.focus)
	}
}

func TestSemantic_DonePopulatesResultsAndSimilarity(t *testing.T) {
	m := semanticModel()
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	// Pretend an embed+walk finished, results already sorted by similarity desc.
	m = drive(m, semanticDoneMsg{
		query: "auth",
		results: []search.Result{
			semRes("a.md", 0.92, true),
			semRes("b.md", 0.70, true),
			semRes("c.md", 0.40, true),
		},
	})

	if m.searching {
		t.Error("searching should clear once semanticDoneMsg lands")
	}
	if !m.hasSimilarity {
		t.Error("hasSimilarity should be set after a semantic walk")
	}
	if len(m.matched) != 3 {
		t.Fatalf("empty CEL filter should match all 3, got %d", len(m.matched))
	}
}

func TestSemantic_CELFilterOnSimilarity(t *testing.T) {
	m := semanticModel()
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drive(m, semanticDoneMsg{results: []search.Result{
		semRes("a.md", 0.92, true),
		semRes("b.md", 0.70, true),
		semRes("c.md", 0.40, true),
	}})

	// Move focus to the CEL box and type a similarity predicate.
	m = drive(m, key("tab"))
	if m.focus != focusCEL {
		t.Fatalf("tab should move focus to the CEL box, got %v", m.focus)
	}
	for _, r := range "similarity > 0.6" {
		m = drive(m, key(string(r)))
	}
	if len(m.matched) != 2 {
		t.Fatalf("similarity > 0.6 should match a.md + b.md (2), got %d", len(m.matched))
	}
}

func TestSemantic_DoneError(t *testing.T) {
	m := semanticModel()
	m = drive(m, semanticDoneMsg{err: context.DeadlineExceeded})
	if m.searchErr == nil {
		t.Error("a failed semantic walk should set searchErr")
	}
	if m.searching {
		t.Error("searching should clear even on error")
	}
}

func TestSemantic_EnterOnQueryStartsSearch(t *testing.T) {
	m := semanticModel()
	m = drive(m, loaded()) // plain snapshot first
	for _, r := range "logging" {
		m = drive(m, key(string(r)))
	}
	if m.sem.Value() != "logging" {
		t.Fatalf("query box should hold the typed text, got %q", m.sem.Value())
	}
	m = drive(m, key("enter"))
	if !m.searching {
		t.Error("enter on the query box should kick off a search (searching=true)")
	}
}

func TestSemantic_EnterDoesNotQuit(t *testing.T) {
	// Regression guard: in semantic mode enter must search/no-op, never quit.
	m := semanticModel()
	m = drive(m, loaded())
	m = drive(m, key("tab")) // focus CEL box
	next, cmd := m.Update(key("enter"))
	_ = next
	if cmd != nil {
		// tea.Quit is a cmd; assert enter on the CEL box produced no cmd.
		t.Error("enter on the CEL box in semantic mode should be a no-op (no quit)")
	}
}

func TestSemantic_TabSwitchesFocusNotSchema(t *testing.T) {
	m := semanticModel()
	m = drive(m, key("tab"))
	if m.focus != focusCEL {
		t.Errorf("tab should switch focus to CEL, got %v", m.focus)
	}
	if m.showSchema {
		t.Error("tab must not toggle the schema panel in semantic mode")
	}
	m = drive(m, key("tab"))
	if m.focus != focusSem {
		t.Errorf("tab again should switch focus back to the query box, got %v", m.focus)
	}
}

func TestReproCommand(t *testing.T) {
	tests := []struct {
		name string
		opts RunOptions
		want string
	}{
		{
			name: "basic",
			opts: RunOptions{
				Embedder:            fakeEmbedder{},
				EmbeddingModel:      "all-minilm",
				EmbeddingServer:     defaultServer,
				SimilarityThreshold: 0.5,
				SemanticQuery:       "how auth works",
				Initial:             "is_source && similarity > 0.6",
			},
			want: `file-search-on search --semantic-query 'how auth works' --embedding-model 'all-minilm' --similarity-threshold 0.5 'is_source && similarity > 0.6'`,
		},
		{
			name: "custom server and root",
			opts: RunOptions{
				Embedder:            fakeEmbedder{},
				EmbeddingModel:      "all-minilm",
				EmbeddingServer:     "http://gpu:11434",
				SimilarityThreshold: 0.7,
				SemanticQuery:       "rate limiting",
				Opts:                search.Options{Roots: []string{"./src"}},
			},
			want: `file-search-on search --semantic-query 'rate limiting' --embedding-model 'all-minilm' --embedding-server 'http://gpu:11434' --similarity-threshold 0.7 -d './src'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), tt.opts)
			if got := m.reproCommand(); got != tt.want {
				t.Errorf("reproCommand()\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}

func TestFinalOutput_NonSemanticReturnsExpr(t *testing.T) {
	m := newModel(context.Background(), RunOptions{Initial: "is_pdf"})
	m = drive(m, key("enter")) // records finalExpr & quits
	if got := m.finalOutput(); got != "is_pdf" {
		t.Errorf("non-semantic finalOutput should be the bare CEL expr, got %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":             "''",
		"plain":        "'plain'",
		"a b":          "'a b'",
		"it's":         `'it'\''s'`,
		"a && b > 0.6": `'a && b > 0.6'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
