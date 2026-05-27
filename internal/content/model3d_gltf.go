package content

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

// glTF 2.0 parser — both .gltf (JSON) and .glb (binary container).
//
// .glb layout: 12-byte header (`glTF` magic, uint32 version, uint32
// total length) followed by chunks. Each chunk is uint32 length +
// uint32 type + payload. The first chunk (type `JSON` = 0x4E4F534A)
// is the glTF document; the optional second chunk (`BIN\0`) holds the
// geometry buffers, which we don't read.
//
// We surface, from the JSON document:
//   - materials     — names from materials[].name
//   - has_textures  — len(images) > 0 || len(textures) > 0
//   - has_normals   — any mesh primitive declares a NORMAL attribute
//   - vertex_count  — Σ accessors[primitive.attributes.POSITION].count
//   - face_count    — Σ (primitive.indices ? accessors[indices].count
//                      : POSITION.count) / 3
//   - bounding_box  — union of POSITION accessors' min/max (glTF stores
//                      these directly, so no buffer read is needed)

// gltfDoc is the subset of the glTF 2.0 schema we read.
type gltfDoc struct {
	Accessors []struct {
		Count int64     `json:"count"`
		Type  string    `json:"type"`
		Min   []float64 `json:"min"`
		Max   []float64 `json:"max"`
	} `json:"accessors"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
		} `json:"primitives"`
	} `json:"meshes"`
	Materials []struct {
		Name string `json:"name"`
	} `json:"materials"`
	Images   []json.RawMessage `json:"images"`
	Textures []json.RawMessage `json:"textures"`
}

func parseGLTF(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	isGLB := strings.EqualFold(filepath.Ext(path), ".glb")

	var jsonBytes []byte
	if isGLB {
		b, ok := readGLBJSONChunk(fsys, path)
		if !ok {
			return Attributes{}, nil
		}
		jsonBytes = b
	} else {
		b, err := readAll(fsys, path)
		if err != nil {
			return Attributes{}, nil
		}
		jsonBytes = b
	}

	var doc gltfDoc
	if err := json.Unmarshal(jsonBytes, &doc); err != nil {
		return Attributes{}, nil
	}
	return gltfAttrs(&doc), nil
}

// readGLBJSONChunk extracts the JSON chunk bytes from a .glb file.
// Returns ok=false on a malformed / truncated container.
func readGLBJSONChunk(fsys fs.FS, path string) ([]byte, bool) {
	rs, _, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = closer() }()

	var header [12]byte
	if _, err := io.ReadFull(rs, header[:]); err != nil {
		return nil, false
	}
	if string(header[0:4]) != "glTF" {
		return nil, false
	}
	// header[4:8] = version (2), header[8:12] = total length (unused).

	var chunkHdr [8]byte
	if _, err := io.ReadFull(rs, chunkHdr[:]); err != nil {
		return nil, false
	}
	chunkLen := binary.LittleEndian.Uint32(chunkHdr[0:4])
	chunkType := binary.LittleEndian.Uint32(chunkHdr[4:8])
	const glbChunkJSON = 0x4E4F534A // "JSON"
	if chunkType != glbChunkJSON {
		return nil, false
	}
	if chunkLen == 0 || chunkLen > glbMaxJSONChunk {
		return nil, false
	}
	buf := make([]byte, chunkLen)
	if _, err := io.ReadFull(rs, buf); err != nil {
		return nil, false
	}
	return buf, true
}

func gltfAttrs(doc *gltfDoc) Attributes {
	var vertexCount, faceCount int64
	hasNormals := false
	var box bbox

	for mi := range doc.Meshes {
		for _, prim := range doc.Meshes[mi].Primitives {
			if _, ok := prim.Attributes["NORMAL"]; ok {
				hasNormals = true
			}
			posIdx, hasPos := prim.Attributes["POSITION"]
			if hasPos && posIdx >= 0 && posIdx < len(doc.Accessors) {
				acc := doc.Accessors[posIdx]
				vertexCount += acc.Count
				// POSITION accessors carry min/max [x,y,z] — union them.
				if len(acc.Min) >= 3 && len(acc.Max) >= 3 {
					box.add(acc.Min[0], acc.Min[1], acc.Min[2])
					box.add(acc.Max[0], acc.Max[1], acc.Max[2])
				}
			}
			// Face count: indexed primitives use the indices accessor;
			// non-indexed use POSITION count. Both are triangle lists
			// for the common case (we don't decode primitive.mode).
			switch {
			case prim.Indices != nil && *prim.Indices >= 0 && *prim.Indices < len(doc.Accessors):
				faceCount += doc.Accessors[*prim.Indices].Count / 3
			case hasPos && posIdx >= 0 && posIdx < len(doc.Accessors):
				faceCount += doc.Accessors[posIdx].Count / 3
			}
		}
	}

	var materials []string
	for _, m := range doc.Materials {
		if m.Name != "" {
			materials = append(materials, m.Name)
		}
	}
	hasTextures := len(doc.Images) > 0 || len(doc.Textures) > 0

	return model3dAttrs("gltf", vertexCount, faceCount, hasNormals, hasTextures, materials, box.slice())
}
