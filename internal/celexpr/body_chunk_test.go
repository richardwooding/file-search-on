package celexpr

import (
	"context"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

func TestChunkSource_FunctionChunksPlusHeader(t *testing.T) {
	body := "package demo\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func add(a, b int) int {\n" + // 5
		"\treturn a + b\n" + // 6
		"}\n" + // 7
		"\n" + // 8
		"func greet() {\n" + // 9
		"\tfmt.Println(\"hi\")\n" + // 10
		"}\n" // 11
	spans := content.FunctionSpans("source/go", []byte(body))

	chunks := chunkSource(body, spans, 8<<10)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (header + 2 funcs): %+v", len(chunks), chunks)
	}
	// Header chunk first: lines 1-4, no symbol.
	if chunks[0].span.Symbol != "" || chunks[0].span.StartLine != 1 {
		t.Errorf("header chunk = %+v, want StartLine 1, empty symbol", chunks[0].span)
	}
	if !strings.Contains(chunks[0].text, "package demo") {
		t.Errorf("header chunk should contain package decl, got %q", chunks[0].text)
	}
	// Function chunks carry symbol + range.
	if chunks[1].span.Symbol != "add" || chunks[1].span.StartLine != 5 || chunks[1].span.EndLine != 7 {
		t.Errorf("add chunk span = %+v, want add 5-7", chunks[1].span)
	}
	if !strings.Contains(chunks[1].text, "return a + b") {
		t.Errorf("add chunk missing body: %q", chunks[1].text)
	}
	if chunks[2].span.Symbol != "greet" || chunks[2].span.StartLine != 9 {
		t.Errorf("greet chunk span = %+v, want greet starting line 9", chunks[2].span)
	}
}

func TestChunkSource_NilWhenNoFunctions(t *testing.T) {
	if got := chunkSource("just prose\nno functions\n", nil, 8<<10); got != nil {
		t.Errorf("no spans should yield nil (byte fallback), got %+v", got)
	}
}

func TestBuildEmbedChunks_NonSourceUsesByteWindows(t *testing.T) {
	body := "line one\nline two\nline three\n"
	chunks := buildEmbedChunks("text/plain", body, 8<<10)
	if len(chunks) != 1 {
		t.Fatalf("short text → 1 window, got %d", len(chunks))
	}
	if chunks[0].span.Symbol != "" {
		t.Errorf("byte window must have empty symbol, got %q", chunks[0].span.Symbol)
	}
	if chunks[0].span.StartLine != 1 || chunks[0].span.EndLine != 4 {
		t.Errorf("byte window span = %+v, want lines 1-4", chunks[0].span)
	}
}

func TestByteWindowSpan_LineCounts(t *testing.T) {
	text := "a\nb\nc\nd\ne\n"
	// window covering "b\nc\n" starts after the first newline (offset 2).
	sp := byteWindowSpan(text, 2, 6)
	if sp.StartLine != 2 || sp.EndLine != 4 {
		t.Errorf("span = %+v, want StartLine 2 EndLine 4", sp)
	}
}

// fakeEmbedder returns a fixed vector per call; failOn makes the Nth distinct
// call text fail, exercising the skip-but-stay-aligned path of embedChunks.
type fakeEmbedder struct{ failText string }

func (f fakeEmbedder) Model() string { return "fake" }
func (f fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if f.failText != "" && strings.Contains(text, f.failText) {
		return nil, context.Canceled // any non-nil error; not ctx-derived here
	}
	return []float32{1, 0, 0}, nil
}

func TestEmbedChunks_SkippedChunkKeepsSpansAligned(t *testing.T) {
	chunks := []embedChunk{
		{text: "alpha", span: index.ChunkSpan{StartLine: 1, EndLine: 1, Symbol: "a"}},
		{text: "FAILME beta", span: index.ChunkSpan{StartLine: 2, EndLine: 2, Symbol: "b"}},
		{text: "gamma", span: index.ChunkSpan{StartLine: 3, EndLine: 3, Symbol: "c"}},
	}
	vecs, spans := embedChunks(context.Background(), fakeEmbedder{failText: "FAILME"}, chunks)
	if len(vecs) != 2 || len(spans) != 2 {
		t.Fatalf("got %d vecs / %d spans, want 2 each (middle skipped)", len(vecs), len(spans))
	}
	// The surviving spans must be the non-failing ones, in order.
	if spans[0].Symbol != "a" || spans[1].Symbol != "c" {
		t.Errorf("surviving spans = %v, want symbols a,c", spans)
	}
}
