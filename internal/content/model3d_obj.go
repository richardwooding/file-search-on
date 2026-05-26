package content

import (
	"bufio"
	"context"
	"io/fs"
	"strings"
)

// Wavefront OBJ parser. Line-oriented ASCII:
//
//	v  x y z      geometry vertex
//	vn x y z      vertex normal
//	vt u v        texture coordinate
//	f  ...        face (polygon, by vertex indices)
//	mtllib file   reference to a material library (.mtl)
//	usemtl name   select a named material
//	# comment
//
// Surfaces:
//   - vertex_count   — count of `v ` lines
//   - face_count     — count of `f ` lines
//   - has_normals    — any `vn ` line present
//   - has_textures   — a `mtllib` reference exists (OBJ textures live
//                      in the referenced .mtl, so this flags "has a
//                      material library", per the issue)
//   - materials      — unique `usemtl` names
//   - bounding_box   — min/max over `v ` positions
//
// COFF `.obj` files (Windows compiled objects) share the extension but
// aren't meshes; they produce no v/f lines and the parser returns
// empty attrs.

func parseOBJ(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var verts, faces int64
	hasNormals := false
	hasMtllib := false
	var box bbox
	matSet := make(map[string]struct{})
	var materials []string

	for sc.Scan() {
		if (verts+faces)%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		switch {
		case strings.HasPrefix(line, "v "):
			if verts < modelMaxVertices {
				if x, y, z, ok := parseThreeFloats(line[2:]); ok {
					box.add(x, y, z)
				}
			}
			verts++
		case strings.HasPrefix(line, "f "):
			faces++
		case strings.HasPrefix(line, "vn "):
			hasNormals = true
		case strings.HasPrefix(line, "mtllib"):
			hasMtllib = true
		case strings.HasPrefix(line, "usemtl"):
			name := strings.TrimSpace(strings.TrimPrefix(line, "usemtl"))
			if name != "" {
				if _, seen := matSet[name]; !seen && len(materials) < modelMaxMaterials {
					matSet[name] = struct{}{}
					materials = append(materials, name)
				}
			}
		}
	}

	if verts == 0 && faces == 0 {
		// Not a mesh OBJ (e.g. a COFF object file with the same ext).
		return Attributes{}, nil
	}
	return model3dAttrs("obj", verts, faces, hasNormals, hasMtllib, materials, box.slice()), nil
}
