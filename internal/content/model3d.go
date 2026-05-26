package content

import (
	"context"
	"errors"
	"io/fs"
	"math"
)

// 3D model content types (issue #213). Three formats under the
// `model3d/` family:
//
//	model3d/stl   — STereoLithography (3D printing). ASCII + binary.
//	model3d/obj   — Wavefront OBJ (line-oriented ASCII mesh).
//	model3d/gltf  — glTF 2.0 (.gltf JSON + .glb binary; game / web assets).
//
// All parsers are triage-grade: they surface vertex_count, face_count,
// has_normals, has_textures, materials, and bounding_box where cheap to
// read — never a full mesh validation. Pure Go, no third-party deps.
//
// Mesh-quality checks (manifold / watertight), FBX / PLY / USDZ, and
// texture-content extraction are out of scope.

func init() {
	// STL: extension-only. ASCII STL starts with "solid " but so do
	// some binary STLs (the 80-byte header is free-form), so the
	// ascii-vs-binary decision is made inside the parser via the
	// 84 + 50·n size formula, not by magic.
	Register(&model3dType{name: "model3d/stl", exts: []string{".stl"}, magic: nil})
	// OBJ: extension-only. No reliable magic (collides with COFF .obj
	// on Windows); the parser returns empty attrs for non-mesh content.
	Register(&model3dType{name: "model3d/obj", exts: []string{".obj"}, magic: nil})
	// glTF: .gltf is JSON (extension-only — the `{` prefix would
	// over-fire), .glb is binary with a 4-byte `glTF` magic.
	Register(&model3dType{name: "model3d/gltf", exts: []string{".gltf", ".glb"}, magic: [][]byte{[]byte("glTF")}})
}

type model3dType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (m *model3dType) Name() string         { return m.name }
func (m *model3dType) Extensions() []string { return m.exts }
func (m *model3dType) MagicBytes() [][]byte { return m.magic }

func (m *model3dType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch m.name {
	case "model3d/stl":
		return parseSTL(ctx, fsys, path)
	case "model3d/obj":
		return parseOBJ(ctx, fsys, path)
	case "model3d/gltf":
		return parseGLTF(ctx, fsys, path)
	}
	return nil, errors.New("unsupported model3d type")
}

// Defensive caps. Real models fit well within these; anything larger is
// either malformed or adversarial.
const (
	// modelMaxVertices bounds the vertex/face scan for ASCII STL + OBJ
	// (binary STL counts come free from the header). Streaming + O(1)
	// memory, so this caps TIME not memory. 50M verts ≈ a very dense
	// scan; real triage models are far smaller.
	modelMaxVertices = 50_000_000
	// glbMaxJSONChunk caps the GLB JSON chunk we read into memory.
	glbMaxJSONChunk = 32 << 20
	// modelMaxMaterials bounds the materials list so a pathological
	// file can't blow up the wire shape.
	modelMaxMaterials = 1024
)

// bbox accumulates an axis-aligned bounding box over a stream of
// (x, y, z) vertex positions. Zero-value is "empty" (no points seen).
type bbox struct {
	minX, minY, minZ float64
	maxX, maxY, maxZ float64
	any              bool
}

func (b *bbox) add(x, y, z float64) {
	if !b.any {
		b.minX, b.maxX = x, x
		b.minY, b.maxY = y, y
		b.minZ, b.maxZ = z, z
		b.any = true
		return
	}
	b.minX = math.Min(b.minX, x)
	b.minY = math.Min(b.minY, y)
	b.minZ = math.Min(b.minZ, z)
	b.maxX = math.Max(b.maxX, x)
	b.maxY = math.Max(b.maxY, y)
	b.maxZ = math.Max(b.maxZ, z)
}

// slice returns the [minX,minY,minZ,maxX,maxY,maxZ] form for the
// bounding_box attribute, or nil when no points were seen.
func (b *bbox) slice() []float64 {
	if !b.any {
		return nil
	}
	return []float64{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ}
}

// model3dAttrs packs the surface. format is "stl" / "obj" / "gltf".
// Empty / zero fields are omitted so sparse models stay clean on the
// JSON wire. The cross-format `model3d_format` is always present.
func model3dAttrs(format string, vertexCount, faceCount int64, hasNormals, hasTextures bool, materials []string, box []float64) Attributes {
	out := Attributes{"model3d_format": format}
	if vertexCount > 0 {
		out["vertex_count"] = vertexCount
	}
	if faceCount > 0 {
		out["face_count"] = faceCount
	}
	if hasNormals {
		out["has_normals"] = true
	}
	if hasTextures {
		out["has_textures"] = true
	}
	if len(materials) > 0 {
		if len(materials) > modelMaxMaterials {
			materials = materials[:modelMaxMaterials]
		}
		out["materials"] = materials
	}
	if len(box) == 6 {
		out["bounding_box"] = box
	}
	return out
}
