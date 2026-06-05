package vecindex

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/richardwooding/file-search-on/internal/embed"
)

func norm(v []float32) []float32 {
	embed.Normalize(v)
	return v
}

func TestSearch_FindsNearestDescending(t *testing.T) {
	idx := New()
	idx.Add("x", norm([]float32{1, 0, 0}))
	idx.Add("y", norm([]float32{0, 1, 0}))
	idx.Add("z", norm([]float32{0, 0, 1}))

	got := idx.Search(norm([]float32{0.9, 0.2, 0}), 3)
	if len(got) != 3 || got[0].Key != "x" {
		t.Fatalf("nearest = %+v, want x first", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Similarity > got[i-1].Similarity {
			t.Errorf("not sorted desc: %+v", got)
		}
	}
}

func TestAddDeleteHasLen(t *testing.T) {
	idx := New()
	idx.Add("a", norm([]float32{1, 0}))
	idx.Add("b", norm([]float32{0, 1}))
	if idx.Len() != 2 || !idx.Has("a") {
		t.Fatalf("Len=%d Has(a)=%v", idx.Len(), idx.Has("a"))
	}
	if !idx.Delete("a") || idx.Delete("a") {
		t.Error("Delete should return true then false")
	}
	if idx.Len() != 1 || idx.Has("a") {
		t.Errorf("after delete: Len=%d Has(a)=%v", idx.Len(), idx.Has("a"))
	}
}

func TestAdd_ReplacesAndIgnoresEmpty(t *testing.T) {
	idx := New()
	idx.Add("a", norm([]float32{1, 0}))
	idx.Add("a", norm([]float32{0, 1})) // replace
	idx.Add("empty", nil)
	if idx.Len() != 1 {
		t.Errorf("Len=%d want 1 (replace + ignore empty)", idx.Len())
	}
	got := idx.Search(norm([]float32{0, 1}), 1)
	if got[0].Similarity < 0.99 {
		t.Errorf("replacement vector not stored; sim=%.3f", got[0].Similarity)
	}
}

func TestSearch_Empty(t *testing.T) {
	if got := New().Search([]float32{1, 0}, 5); got != nil {
		t.Errorf("empty index = %+v, want nil", got)
	}
	if got := New().Search(nil, 5); got != nil {
		t.Errorf("nil query = %+v, want nil", got)
	}
}

// TestSearch_ExactRecall is the property the whole package exists for:
// 100% recall. A query that's a slightly-perturbed copy of an indexed
// 768-dim vector must always return that vector first.
func TestSearch_ExactRecall(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	randVec := func() []float32 {
		v := make([]float32, 768)
		for d := range v {
			v[d] = float32(rng.NormFloat64())
		}
		return norm(v)
	}
	idx := New()
	const n = 3000
	vecs := make([][]float32, n)
	for i := range n {
		vecs[i] = randVec()
		idx.Add(fmt.Sprintf("r%d", i), vecs[i])
	}
	hits, trials := 0, 300
	for q := range trials {
		base := vecs[q*7%n]
		query := make([]float32, 768)
		copy(query, base)
		for d := range query {
			query[d] += float32(rng.NormFloat64()) * 0.05
		}
		norm(query)
		got := idx.Search(query, 5)
		want := fmt.Sprintf("r%d", q*7%n)
		if len(got) > 0 && got[0].Key == want {
			hits++
		}
	}
	if hits != trials {
		t.Errorf("exact recall@1 = %d/%d; an exact scan must be 100%%", hits, trials)
	}
}

func TestConcurrent(t *testing.T) {
	idx := New()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			idx.Add(fmt.Sprintf("k%d", n), norm([]float32{float32(n) + 1, 1, 0}))
		}(i)
	}
	for range 20 {
		wg.Go(func() {
			_ = idx.Search(norm([]float32{1, 1, 0}), 3)
		})
	}
	wg.Wait()
	if idx.Len() == 0 {
		t.Error("expected vectors after concurrent adds")
	}
}
