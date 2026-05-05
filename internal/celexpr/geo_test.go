package celexpr_test

import (
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

func TestPointInPolygon(t *testing.T) {
	// Cape Town's City Bowl as a rough rectangle. Coords are
	// (lat, lon) pairs going SW -> NW -> NE -> SE.
	cityBowl := []float64{
		-33.96, 18.40,
		-33.91, 18.40,
		-33.91, 18.45,
		-33.96, 18.45,
	}

	cases := []struct {
		name      string
		lat, lon  float64
		polygon   []float64
		want      bool
	}{
		// Inside Cape Town City Bowl rectangle.
		{"city bowl center", -33.93, 18.42, cityBowl, true},
		// Outside (Stellenbosch — far east).
		{"stellenbosch", -33.93, 18.86, cityBowl, false},
		// Outside (Atlantic ocean — far west).
		{"atlantic", -33.93, 18.30, cityBowl, false},
		// Outside (north of CT).
		{"north", -33.50, 18.42, cityBowl, false},
		// Triangle: (0,0), (0,10), (10,0). Point (1,1) is inside.
		{"triangle inside", 1, 1, []float64{0, 0, 0, 10, 10, 0}, true},
		// Triangle, point (5,5) — exactly on hypotenuse, technically on edge,
		// but ray casting will resolve it consistently. Skip strict on-edge
		// tests; this sample is comfortably inside instead.
		{"triangle inside near edge", 4, 4, []float64{0, 0, 0, 10, 10, 0}, true},
		// Triangle, point outside the hypotenuse.
		{"triangle outside", 6, 6, []float64{0, 0, 0, 10, 10, 0}, false},
		// Concave polygon: a U-shape. Point in the well of the U is OUTSIDE.
		// Vertices (lat, lon):
		//   (0,0), (10,0), (10,2), (2,2), (2,8), (10,8), (10,10), (0,10)
		{"u-shape inside the well (outside polygon)", 5, 5,
			[]float64{0, 0, 10, 0, 10, 2, 2, 2, 2, 8, 10, 8, 10, 10, 0, 10},
			false},
		{"u-shape inside the left arm (inside polygon)", 1, 5,
			[]float64{0, 0, 10, 0, 10, 2, 2, 2, 2, 8, 10, 8, 10, 10, 0, 10},
			true},
		// Degenerate: empty polygon.
		{"empty polygon", 0, 0, []float64{}, false},
		// Degenerate: 2 points (a line, not a polygon).
		{"two-point polygon", 0, 0, []float64{0, 0, 1, 1}, false},
		// Degenerate: odd number of floats (malformed input).
		{"odd-length coords", 0, 0, []float64{0, 0, 1, 1, 2}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := celexpr.PointInPolygon(tc.lat, tc.lon, tc.polygon)
			if got != tc.want {
				t.Errorf("PointInPolygon(%v, %v, %v) = %v; want %v", tc.lat, tc.lon, tc.polygon, got, tc.want)
			}
		})
	}
}

func TestEvaluatePointInPolygon(t *testing.T) {
	// Image with GPS in Cape Town.
	attrs := &celexpr.FileAttributes{
		Name:        "ct-photo.jpg",
		Path:        "/photos/ct-photo.jpg",
		Dir:         "/photos",
		Size:        4096,
		Ext:         ".jpg",
		ContentType: "image/jpeg",
		IsImage:     true,
		Extra: map[string]any{
			"gps_lat": float64(-33.93),
			"gps_lon": float64(18.42),
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		// Inside a Cape Town bounding rectangle.
		{`is_image && point_in_polygon(gps_lat, gps_lon, [-33.96, 18.40, -33.91, 18.40, -33.91, 18.45, -33.96, 18.45])`, true},
		// Outside (Joburg-ish region).
		{`is_image && point_in_polygon(gps_lat, gps_lon, [-26.30, 27.90, -26.10, 27.90, -26.10, 28.20, -26.30, 28.20])`, false},
		// Loose polygon written as doubles (CEL is strict about list element type — int literals won't cast).
		{`is_image && point_in_polygon(gps_lat, gps_lon, [-34.0, 18.0, -33.0, 18.0, -33.0, 19.0, -34.0, 19.0])`, true},
	}
	for _, tc := range cases {
		eval, err := celexpr.New(tc.expr)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.expr, err)
		}
		got, err := eval.Evaluate(attrs)
		if err != nil {
			t.Fatalf("eval %q: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("expr %q: got %v, want %v", tc.expr, got, tc.want)
		}
	}
}
