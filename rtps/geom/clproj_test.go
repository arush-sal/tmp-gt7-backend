package geom

import (
	"math"
	"testing"
)

func TestClArcLengths(t *testing.T) {
	// (0,0)->(3,0)->(3,4): segment lengths 3 then 4 -> cumulative 0, 3, 7.
	arc := ClArcLengths([][2]float64{{0, 0}, {3, 0}, {3, 4}})
	want := []float64{0, 3, 7}
	if len(arc) != len(want) {
		t.Fatalf("len = %d, want %d", len(arc), len(want))
	}
	for i := range want {
		if math.Abs(arc[i]-want[i]) > 1e-9 {
			t.Errorf("arc[%d] = %v, want %v", i, arc[i], want[i])
		}
	}
	if ClArcLengths(nil) != nil {
		t.Error("ClArcLengths(nil) should be nil")
	}
}

func TestProjectToClArc(t *testing.T) {
	// cl (0,0)->(10,0). Point (3,2) foot-of-perpendicular at (3,0) -> arc 3.
	// Nearest-VERTEX would snap to (0,0) -> arc 0; segment interp must give 3.
	cl := [][2]float64{{0, 0}, {10, 0}}
	arc := ClArcLengths(cl)
	got, ok := ProjectToClArc(cl, arc, 3, 2)
	if !ok {
		t.Fatal("ok=false")
	}
	if math.Abs(got-3) > 1e-9 {
		t.Fatalf("mid-segment arc = %v, want 3", got)
	}

	// Endpoint: (10,0) projects to arc 10 (the polyline total).
	if got, _ := ProjectToClArc(cl, arc, 10, 0); math.Abs(got-10) > 1e-9 {
		t.Errorf("endpoint arc = %v, want 10", got)
	}

	// Fewer than two vertices -> ok=false.
	if _, ok := ProjectToClArc([][2]float64{{0, 0}}, []float64{0}, 1, 1); ok {
		t.Error("single-vertex polyline should return ok=false")
	}
}
