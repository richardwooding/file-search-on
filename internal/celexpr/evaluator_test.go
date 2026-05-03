package celexpr_test

import (
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

func TestEvaluate(t *testing.T) {
	eval, err := celexpr.New("size > 100 && is_json")
	if err != nil {
		t.Fatal(err)
	}

	attrs := &celexpr.FileAttributes{
		Name:        "test.json",
		Path:        "/home/runner/work/file-search-on/file-search-on/test.json",
		Dir:         "/home/runner/work/file-search-on/file-search-on",
		Size:        200,
		Ext:         ".json",
		ContentType: "json",
		IsJSON:      true,
	}

	match, err := eval.Evaluate(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected match, got no match")
	}
}

func TestEvaluateFalse(t *testing.T) {
	eval, err := celexpr.New("size > 100 && is_json")
	if err != nil {
		t.Fatal(err)
	}

	attrs := &celexpr.FileAttributes{
		Name:        "test.txt",
		Path:        "/home/runner/work/file-search-on/file-search-on/test.txt",
		Dir:         "/home/runner/work/file-search-on/file-search-on",
		Size:        50,
		Ext:         ".txt",
		ContentType: "",
		IsJSON:      false,
	}

	match, err := eval.Evaluate(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if match {
		t.Error("expected no match, got match")
	}
}
