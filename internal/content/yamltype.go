package content

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"gopkg.in/yaml.v3"
)

func init() {
	Register(&yamlType{})
}

type yamlType struct{}

func (y *yamlType) Name() string         { return "yaml" }
func (y *yamlType) Extensions() []string { return []string{".yaml", ".yml"} }

// MagicBytes returns nil — YAML has no canonical magic byte. The
// optional %YAML directive exists but is rare; relying on it would
// miss the bulk of real-world YAML (CI configs, K8s manifests,
// GoReleaser). Extension-only detection is the right tradeoff.
func (y *yamlType) MagicBytes() [][]byte { return nil }

// Attributes returns the YAML root-node kind and document count.
// Kind is one of "object" (mapping), "array" (sequence), "scalar"
// (string / int / etc. at the root), or "unknown" (parse failure or
// empty input). DocumentCount counts the `---`-separated documents
// in the file (1 for the common single-doc case).
//
// Parses the file streamingly with yaml.Decoder: peeks at the first
// document's root node to determine kind, then advances through any
// remaining documents just to count them. Doesn't materialise the
// full tree, so multi-megabyte K8s manifests stay cheap.
func (y *yamlType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	var first yaml.Node
	if err := dec.Decode(&first); err != nil {
		// Empty file or parse error — distinguished from "valid but
		// empty mapping" because we never see a node.
		return Attributes{
			"yaml_kind":           "unknown",
			"yaml_document_count": int64(0),
		}, nil
	}

	kind := yamlNodeKind(&first)
	docs := int64(1)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var next yaml.Node
		if err := dec.Decode(&next); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Mid-stream parse failure — return what we have so far
			// rather than failing the walk. The kind from doc 1 is
			// still meaningful.
			break
		}
		docs++
	}

	return Attributes{
		"yaml_kind":           kind,
		"yaml_document_count": docs,
	}, nil
}

// yamlNodeKind maps a yaml.Node to one of the four kind strings
// surfaced to CEL. yaml.Node wraps documents in a DocumentNode whose
// single child is the actual root; we unwrap to get the real shape.
func yamlNodeKind(n *yaml.Node) string {
	if n == nil {
		return "unknown"
	}
	root := n
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	switch root.Kind {
	case yaml.MappingNode:
		return "object"
	case yaml.SequenceNode:
		return "array"
	case yaml.ScalarNode:
		return "scalar"
	default:
		return "unknown"
	}
}
