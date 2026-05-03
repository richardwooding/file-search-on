package content

import (
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

func (j *jsonType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	tok, err := decoder.Token()
	if err != nil {
		return Attributes{"kind": "unknown"}, nil
	}
	kind := "unknown"
	if d, ok := tok.(json.Delim); ok {
		if d == '{' {
			kind = "object"
		} else if d == '[' {
			kind = "array"
		}
	}
	return Attributes{
		"kind": kind,
	}, nil
}
