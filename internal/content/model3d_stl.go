package content

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"math"
	"strconv"
	"strings"
)

// STL parser — both the binary and ASCII variants.
//
// Binary STL: 80-byte free-form header, uint32 little-endian triangle
// count, then 50 bytes per triangle (12 floats: normal + 3 vertices,
// + 2-byte attribute). The triangle count is O(1) from the header;
// vertex_count = 3·count, face_count = count. The bounding box needs a
// pass over the triangle vertices.
//
// ASCII STL: `solid <name>` … `facet normal nx ny nz` / `outer loop` /
// `vertex x y z` ×3 / `endloop` / `endfacet` … `endsolid`. Counts +
// bbox come from a line scan.
//
// The ascii-vs-binary decision uses the binary size formula
// (84 + 50·n == fileSize) rather than the "solid" prefix, since some
// binary writers put "solid" in the 80-byte header too.

func parseSTL(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return Attributes{}, nil
	}
	defer func() { _ = closer() }()

	if isBinarySTL(rs, size) {
		return parseBinarySTL(ctx, rs, size)
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return Attributes{}, nil
	}
	return parseASCIISTL(ctx, rs)
}

// isBinarySTL reports whether the stream is a binary STL by checking
// the 84 + 50·triangleCount == fileSize invariant. Resets the seek
// position to 0 before returning.
func isBinarySTL(rs io.ReadSeeker, size int64) bool {
	if size < 84 {
		return false
	}
	var head [84]byte
	if _, err := io.ReadFull(rs, head[:]); err != nil {
		_, _ = rs.Seek(0, io.SeekStart)
		return false
	}
	_, _ = rs.Seek(0, io.SeekStart)
	count := binary.LittleEndian.Uint32(head[80:84])
	return int64(84)+int64(count)*50 == size
}

func parseBinarySTL(ctx context.Context, rs io.ReadSeeker, size int64) (Attributes, error) {
	var head [84]byte
	if _, err := io.ReadFull(rs, head[:]); err != nil {
		return Attributes{}, nil
	}
	count := int64(binary.LittleEndian.Uint32(head[80:84]))
	if count <= 0 {
		return model3dAttrs("stl", 0, 0, true, false, nil, nil), nil
	}

	// Stream the 50-byte triangle records, accumulating the bounding
	// box. Bounded by modelMaxVertices worth of triangles.
	var box bbox
	br := bufio.NewReader(rs)
	var tri [50]byte
	scanned := int64(0)
	maxTris := int64(modelMaxVertices / 3)
	for i := int64(0); i < count && i < maxTris; i++ {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if _, err := io.ReadFull(br, tri[:]); err != nil {
			break // truncated; report what the header claimed + partial bbox
		}
		// Bytes 12..48 are the three vertices (9 float32 LE). Bytes
		// 0..12 are the facet normal (skipped for the bbox).
		for v := range 3 {
			off := 12 + v*12
			x := math.Float32frombits(binary.LittleEndian.Uint32(tri[off : off+4]))
			y := math.Float32frombits(binary.LittleEndian.Uint32(tri[off+4 : off+8]))
			z := math.Float32frombits(binary.LittleEndian.Uint32(tri[off+8 : off+12]))
			box.add(float64(x), float64(y), float64(z))
		}
		scanned++
	}
	_ = scanned
	// Binary STL always carries per-facet normals.
	return model3dAttrs("stl", count*3, count, true, false, nil, box.slice()), nil
}

func parseASCIISTL(ctx context.Context, r io.Reader) (Attributes, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var faces, verts int64
	var box bbox
	hasNormals := false
	for sc.Scan() {
		if faces%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "facet normal"):
			faces++
			hasNormals = true
		case strings.HasPrefix(line, "vertex "):
			if verts >= modelMaxVertices {
				continue
			}
			if x, y, z, ok := parseThreeFloats(line[len("vertex "):]); ok {
				box.add(x, y, z)
			}
			verts++
		}
	}
	if faces == 0 && verts == 0 {
		// Not actually an STL (e.g. a stray .stl that isn't a mesh).
		return Attributes{}, nil
	}
	return model3dAttrs("stl", verts, faces, hasNormals, false, nil, box.slice()), nil
}

// parseThreeFloats parses the first three whitespace-separated floats
// from s. Used for both STL `vertex x y z` and OBJ `v x y z` lines.
func parseThreeFloats(s string) (x, y, z float64, ok bool) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	var err error
	if x, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return 0, 0, 0, false
	}
	if y, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return 0, 0, 0, false
	}
	if z, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return 0, 0, 0, false
	}
	return x, y, z, true
}
