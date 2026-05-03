package content

import (
	"context"
	"encoding/json"
	"os"
)

func init() {
	Register(&jsonType{})
}

type jsonType struct{}

func (j *jsonType) Name() string { return "json" }
func (j *jsonType) Extensions() []string {
	return []string{".json", ".jsonl", ".geojson"}
}
func (j *jsonType) MagicBytes() [][]byte {
	return [][]byte{
		[]byte("{"),
		[]byte("["),
	}
}

func (j *jsonType) Attributes(ctx context.Context, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	decoder := json.NewDecoder(f)
	tok, err := decoder.Token()
	if err != nil {
		return Attributes{"kind": "unknown"}, nil
	}
	kind := "unknown"
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			kind = "object"
		case '[':
			kind = "array"
		}
	}
	return Attributes{
		"kind": kind,
	}, nil
}
