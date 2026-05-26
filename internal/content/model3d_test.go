package content_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// buildBinarySTL synthesises a binary STL with n triangles. Each
// triangle's three vertices step along the axes so the bounding box is
// predictable. Returns the bytes (84 + 50*n).
func buildBinarySTL(n uint32) []byte {
	buf := make([]byte, 84+int(n)*50)
	// header[0:80] free-form; header[80:84] triangle count.
	binary.LittleEndian.PutUint32(buf[80:84], n)
	for i := range n {
		off := 84 + int(i)*50
		// normal (3 float32) left zero; 3 vertices follow at off+12.
		putVec3(buf[off+12:], 0, 0, 0)
		putVec3(buf[off+24:], float32(i+1), 0, 0)
		putVec3(buf[off+36:], 0, float32(i+1), 0)
		// attribute byte count at off+48 left zero.
	}
	return buf
}

func putVec3(b []byte, x, y, z float32) {
	binary.LittleEndian.PutUint32(b[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(b[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(b[8:12], math.Float32bits(z))
}

func TestSTLBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cube.stl")
	if err := os.WriteFile(path, buildBinarySTL(12), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if attrs["model3d_format"] != "stl" {
		t.Errorf("model3d_format = %v", attrs["model3d_format"])
	}
	if attrs["face_count"] != int64(12) {
		t.Errorf("face_count = %v, want 12", attrs["face_count"])
	}
	if attrs["vertex_count"] != int64(36) {
		t.Errorf("vertex_count = %v, want 36", attrs["vertex_count"])
	}
	if attrs["has_normals"] != true {
		t.Errorf("has_normals = %v, want true", attrs["has_normals"])
	}
	if bb, ok := attrs["bounding_box"].([]float64); !ok || len(bb) != 6 {
		t.Errorf("bounding_box = %v, want a 6-element slice", attrs["bounding_box"])
	}
}

func TestSTLAscii(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tri.stl")
	ascii := "solid t\n" +
		"facet normal 0 0 1\n outer loop\n  vertex 0 0 0\n  vertex 2 0 0\n  vertex 0 3 0\n endloop\nendfacet\n" +
		"endsolid t\n"
	if err := os.WriteFile(path, []byte(ascii), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if attrs["model3d_format"] != "stl" {
		t.Errorf("model3d_format = %v", attrs["model3d_format"])
	}
	if attrs["face_count"] != int64(1) {
		t.Errorf("face_count = %v, want 1", attrs["face_count"])
	}
	if attrs["vertex_count"] != int64(3) {
		t.Errorf("vertex_count = %v, want 3", attrs["vertex_count"])
	}
	bb, ok := attrs["bounding_box"].([]float64)
	if !ok || len(bb) != 6 {
		t.Fatalf("bounding_box = %v", attrs["bounding_box"])
	}
	want := []float64{0, 0, 0, 2, 3, 0}
	for i := range want {
		if bb[i] != want[i] {
			t.Errorf("bounding_box[%d] = %v, want %v", i, bb[i], want[i])
		}
	}
}

func TestOBJ(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quad.obj")
	obj := "# quad\nmtllib quad.mtl\n" +
		"v 0 0 0\nv 1 0 0\nv 1 1 0\nv 0 1 0\n" +
		"vn 0 0 1\n" +
		"usemtl red\nusemtl red\nusemtl blue\n" +
		"f 1 2 3 4\n"
	if err := os.WriteFile(path, []byte(obj), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if attrs["model3d_format"] != "obj" {
		t.Errorf("model3d_format = %v", attrs["model3d_format"])
	}
	if attrs["vertex_count"] != int64(4) {
		t.Errorf("vertex_count = %v, want 4", attrs["vertex_count"])
	}
	if attrs["face_count"] != int64(1) {
		t.Errorf("face_count = %v, want 1", attrs["face_count"])
	}
	if attrs["has_normals"] != true {
		t.Errorf("has_normals = %v, want true", attrs["has_normals"])
	}
	if attrs["has_textures"] != true {
		t.Errorf("has_textures = %v, want true (mtllib present)", attrs["has_textures"])
	}
	mats, ok := attrs["materials"].([]string)
	if !ok || len(mats) != 2 { // red + blue, de-duped
		t.Errorf("materials = %v, want [red blue]", attrs["materials"])
	}
}

func TestOBJNonMeshReturnsEmpty(t *testing.T) {
	// A .obj with no v/f lines (e.g. a COFF object) yields empty attrs.
	dir := t.TempDir()
	path := filepath.Join(dir, "obj.obj")
	if err := os.WriteFile(path, []byte("not a mesh\nrandom text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for non-mesh .obj, got %v", attrs)
	}
}

func TestGLTFJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.gltf")
	gltf := `{"asset":{"version":"2.0"},
	  "accessors":[{"count":24,"type":"VEC3","min":[-1,-2,-3],"max":[4,5,6]},{"count":36,"type":"SCALAR"}],
	  "meshes":[{"primitives":[{"attributes":{"POSITION":0,"NORMAL":0},"indices":1}]}],
	  "materials":[{"name":"Steel"}],
	  "images":[{"uri":"tex.png"}]}`
	if err := os.WriteFile(path, []byte(gltf), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if attrs["model3d_format"] != "gltf" {
		t.Errorf("model3d_format = %v", attrs["model3d_format"])
	}
	if attrs["vertex_count"] != int64(24) {
		t.Errorf("vertex_count = %v, want 24", attrs["vertex_count"])
	}
	if attrs["face_count"] != int64(12) { // 36 indices / 3
		t.Errorf("face_count = %v, want 12", attrs["face_count"])
	}
	if attrs["has_normals"] != true {
		t.Errorf("has_normals = %v, want true", attrs["has_normals"])
	}
	if attrs["has_textures"] != true {
		t.Errorf("has_textures = %v, want true (image present)", attrs["has_textures"])
	}
	if mats, ok := attrs["materials"].([]string); !ok || len(mats) != 1 || mats[0] != "Steel" {
		t.Errorf("materials = %v, want [Steel]", attrs["materials"])
	}
	bb, ok := attrs["bounding_box"].([]float64)
	if !ok {
		t.Fatalf("bounding_box missing")
	}
	want := []float64{-1, -2, -3, 4, 5, 6}
	for i := range want {
		if bb[i] != want[i] {
			t.Errorf("bounding_box[%d] = %v, want %v", i, bb[i], want[i])
		}
	}
}

func TestGLB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.glb")
	jsonChunk := []byte(`{"asset":{"version":"2.0"},"accessors":[{"count":8,"type":"VEC3","min":[0,0,0],"max":[1,1,1]}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"materials":[{"name":"M"}]}`)
	// Pad the JSON chunk to a 4-byte boundary with spaces (glTF spec).
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	var glb []byte
	glb = append(glb, []byte("glTF")...)
	glb = binary.LittleEndian.AppendUint32(glb, 2)                           // version
	glb = binary.LittleEndian.AppendUint32(glb, uint32(12+8+len(jsonChunk))) // total length
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(jsonChunk)))      // chunk length
	glb = binary.LittleEndian.AppendUint32(glb, 0x4E4F534A)                  // "JSON"
	glb = append(glb, jsonChunk...)
	if err := os.WriteFile(path, glb, 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := modelAttrs(t, path)
	if attrs["model3d_format"] != "gltf" {
		t.Errorf("model3d_format = %v (want gltf)", attrs["model3d_format"])
	}
	if attrs["vertex_count"] != int64(8) {
		t.Errorf("vertex_count = %v, want 8", attrs["vertex_count"])
	}
	if mats, ok := attrs["materials"].([]string); !ok || len(mats) != 1 {
		t.Errorf("materials = %v, want [M]", attrs["materials"])
	}
}

func TestModel3DRegistryDetection(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"a.stl", []byte("solid x\nendsolid x\n"), "model3d/stl"},
		{"a.obj", []byte("v 0 0 0\n"), "model3d/obj"},
		{"a.gltf", []byte(`{"asset":{}}`), "model3d/gltf"},
		{"a.glb", append([]byte("glTF"), make([]byte, 8)...), "model3d/gltf"},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil || ct.Name() != tc.want {
				t.Errorf("Detect(%s) = %v, want %s", tc.name, ct, tc.want)
			}
		})
	}
}

func modelAttrs(t *testing.T, path string) content.Attributes {
	t.Helper()
	ct := detectAt(path)
	if ct == nil {
		t.Fatalf("Detect: nil for %s", path)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	return attrs
}
