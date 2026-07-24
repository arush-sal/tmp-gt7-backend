// Package geom holds pure geometry helpers for projecting world points onto a
// centerline polyline by arc length. It is a leaf package (depends only on math)
// so any layer — rkbref (sector boundaries) or processing (per-lap sample
// fractions) — can put positions on the SAME centerline-arc axis.
package geom

import "math"

// ClArcLengths returns the cumulative arc length at each polyline vertex
// (arc[0] = 0). Empty input yields nil.
func ClArcLengths(cl [][2]float64) []float64 {
	if len(cl) == 0 {
		return nil
	}
	arc := make([]float64, len(cl))
	for i := 1; i < len(cl); i++ {
		arc[i] = arc[i-1] + math.Hypot(cl[i][0]-cl[i-1][0], cl[i][1]-cl[i-1][1])
	}
	return arc
}

// ProjectToClArc returns the arc length of the foot-of-perpendicular projection
// of (px,pz) onto the nearest centerline segment (segment interpolation, not
// nearest-vertex). arc must be ClArcLengths(cl). ok is false for a polyline with
// fewer than two vertices.
func ProjectToClArc(cl [][2]float64, arc []float64, px, pz float64) (float64, bool) {
	if len(cl) < 2 {
		return 0, false
	}
	best := math.Inf(1)
	var bestArc float64
	for i := 0; i+1 < len(cl); i++ {
		ax, az := cl[i][0], cl[i][1]
		dx, dz := cl[i+1][0]-ax, cl[i+1][1]-az
		segLen2 := dx*dx + dz*dz
		var t float64 // degenerate zero-length segment -> t stays 0 (project to the vertex)
		if segLen2 > 0 {
			t = math.Max(0, math.Min(1, ((px-ax)*dx+(pz-az)*dz)/segLen2))
		}
		fx, fz := ax+t*dx, az+t*dz
		if d := math.Hypot(px-fx, pz-fz); d < best {
			best = d
			bestArc = arc[i] + t*math.Hypot(dx, dz)
		}
	}
	return bestArc, true
}
