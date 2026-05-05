package celexpr

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// geoFunctions returns the cel.EnvOption set for geographic helpers.
// Currently a single primitive: point_in_polygon for filtering by
// arbitrary GPS-coordinate boundaries (richer than rectangular
// gps_lat / gps_lon bounding-box checks). Adding new geo functions
// follows the same three-call-site pattern as fuzzyFunctions:
// implement, declare here, schema-doc in schema.go.
func geoFunctions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("point_in_polygon",
			cel.Overload("point_in_polygon_double_double_list",
				[]*cel.Type{
					cel.DoubleType,
					cel.DoubleType,
					cel.ListType(cel.DoubleType),
				},
				cel.BoolType,
				cel.FunctionBinding(pointInPolygonBinding),
			),
		),
	}
}

func pointInPolygonBinding(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("point_in_polygon: expected 3 args, got %d", len(args))
	}
	lat, ok := args[0].Value().(float64)
	if !ok {
		return types.NewErr("point_in_polygon: expected double for arg 1 (lat), got %T", args[0].Value())
	}
	lon, ok := args[1].Value().(float64)
	if !ok {
		return types.NewErr("point_in_polygon: expected double for arg 2 (lon), got %T", args[1].Value())
	}
	coords, err := celListToFloat64s(args[2])
	if err != nil {
		return types.NewErr("point_in_polygon: arg 3 (polygon): %v", err)
	}
	return types.Bool(PointInPolygon(lat, lon, coords))
}

// celListToFloat64s converts a CEL list<double> argument to []float64.
// Accepts ints in addition to doubles so callers can write polygon
// coordinates without forcing decimal points (e.g. [-34, 18, -33, 18]).
func celListToFloat64s(v ref.Val) ([]float64, error) {
	lst, ok := v.(traits.Lister)
	if !ok {
		return nil, fmt.Errorf("expected list, got %T", v)
	}
	size, ok := lst.Size().(types.Int)
	if !ok {
		return nil, fmt.Errorf("list size not int")
	}
	out := make([]float64, 0, int(size))
	it := lst.Iterator()
	for it.HasNext() == types.True {
		e := it.Next()
		switch x := e.(type) {
		case types.Double:
			out = append(out, float64(x))
		case types.Int:
			out = append(out, float64(int64(x)))
		default:
			return nil, fmt.Errorf("expected double in list, got %T", e)
		}
	}
	return out, nil
}

// PointInPolygon returns true if (lat, lon) lies inside the polygon
// described by `coords`, a flat slice of alternating lat,lon pairs:
//
//	[lat0, lon0, lat1, lon1, ..., latN, lonN]
//
// The polygon does not need to be explicitly closed (the algorithm
// wraps from vertex N back to vertex 0). Returns false for fewer than
// 3 vertices or an odd-length coords slice.
//
// Implementation: classic even-odd ray-casting. Treats coordinates as
// planar (lat as Y, lon as X) which is correct for small polygons
// where curvature of the earth is negligible — covers neighbourhoods,
// cities, and most countries. For very large or near-pole polygons
// callers should pre-project. Points exactly on an edge are
// undefined (the algorithm's behaviour at vertices and edges is
// numerically unstable, but real GPS coordinates almost never hit
// edges exactly).
func PointInPolygon(lat, lon float64, coords []float64) bool {
	if len(coords) < 6 || len(coords)%2 != 0 {
		return false
	}
	n := len(coords) / 2
	inside := false
	j := n - 1
	for i := range n {
		yi, xi := coords[2*i], coords[2*i+1]
		yj, xj := coords[2*j], coords[2*j+1]
		if (yi > lat) != (yj > lat) &&
			lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}
