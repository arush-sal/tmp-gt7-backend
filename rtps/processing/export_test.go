package processing

import (
	"math"
	"time"

	"github.com/arush-sal/garage/backend/rtps/rkb"
)

// This file is compiled only in test builds, so these expose the unexported bbox pre-filter to the
// external processing_test package (which lives out-of-package to avoid an rtps/live import cycle)
// WITHOUT leaking test-only symbols into the production API.

// BboxToleranceForTest exposes the bboxTolerance const to tests.
const BboxToleranceForTest = bboxTolerance

// CandidateDiag is one candidate track's diagnostics for a driven path, used only by the real-capture
// threshold-tuning test to log why a lap resolved (or did not) against the full RKB corpus.
type CandidateDiag struct {
	Slug     string
	Score    float64 // mean nearest-neighbour distance (world metres) against the sampled driven path
	HasBbox  bool
	BboxPass bool    // whether the driven bbox is contained in this candidate's bbox (widened by bboxTolerance)
	MaxPoke  float64 // max metres the driven bbox exceeds this candidate's bbox on any side (<=0 == fully contained, pre-tolerance)
}

// CandidateDiagnosticsForTest scores every candidate geom against the driven path using the SAME
// distance sampling and bbox tolerance as the detector, returning per-candidate diagnostics sorted by
// score ascending. Test-only: lets the tuning test see the correct track's rank/score versus the best
// wrong candidate, which is what the thresholds must separate.
func CandidateDiagnosticsForTest(points [][2]float64, geoms []rkb.TrackGeom) []CandidateDiag {
	drivenBbox := computeBbox(points)
	sampled := samplePath(points)
	out := make([]CandidateDiag, 0, len(geoms))
	for _, g := range geoms {
		d := CandidateDiag{
			Slug:     g.Slug,
			Score:    meanNearestNeighborDistance(sampled, g.Points),
			HasBbox:  g.HasBbox,
			BboxPass: !g.HasBbox || bboxContains(drivenBbox, g.Bbox, bboxTolerance),
		}
		if g.HasBbox {
			// How far (metres) the driven bbox pokes past the candidate bbox on its worst side, before
			// tolerance: max(cand_min - driven_min, driven_max - cand_max) over x and z.
			d.MaxPoke = math.Max(
				math.Max(g.Bbox[0]-drivenBbox[0], drivenBbox[2]-g.Bbox[2]),
				math.Max(g.Bbox[1]-drivenBbox[1], drivenBbox[3]-g.Bbox[3]),
			)
		}
		out = append(out, d)
	}
	sortDiags(out)
	return out
}

// sortDiags sorts diagnostics by ascending score (stable enough for logging; ties keep input order).
func sortDiags(d []CandidateDiag) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].Score < d[j-1].Score; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// FilterDetectableForTest exposes filterDetectable to the tuning test so it scores against the same
// candidate set the detector uses (needs-capture tracks with no points dropped).
func FilterDetectableForTest(geoms []rkb.TrackGeom) []rkb.TrackGeom {
	return filterDetectable(geoms)
}

// BboxContainsForTest exposes bboxContains to tests.
func BboxContainsForTest(driven, cand [4]float64, tol float64) bool {
	return bboxContains(driven, cand, tol)
}

// ForwardReverseTwinForTest exposes the twin rule (isTwin) to tests, taking plain fields: it reports
// whether a and b are an identical-geometry forward/reverse pair and, if so, the forward slug - the
// last-resort pick decide makes when direction of travel could not tell the two apart.
func ForwardReverseTwinForTest(
	aSlug string, aReverse bool, aBase int, aPoints [][2]float64,
	bSlug string, bReverse bool, bBase int, bPoints [][2]float64,
) (string, bool) {
	a := rkb.TrackGeom{Slug: aSlug, IsReverse: aReverse, Base: aBase, Points: aPoints}
	b := rkb.TrackGeom{Slug: bSlug, IsReverse: bReverse, Base: bBase, Points: bPoints}
	if !isTwin(a, b) {
		return "", false
	}
	if a.IsReverse {
		return b.Slug, true
	}
	return a.Slug, true
}

// computeBbox returns [min_x, min_z, max_x, max_z] over points. Caller guarantees points is non-empty.
func computeBbox(points [][2]float64) [4]float64 {
	b := [4]float64{points[0][0], points[0][1], points[0][0], points[0][1]}
	for _, p := range points[1:] {
		b = [4]float64{math.Min(b[0], p[0]), math.Min(b[1], p[1]), math.Max(b[2], p[0]), math.Max(b[3], p[1])}
	}
	return b
}

// samplePath keeps a point each time the path has moved sampleStepM from the last kept one: the same
// distance sampling the detector applies to live frames.
func samplePath(points [][2]float64) [][2]float64 {
	var out [][2]float64
	for _, p := range points {
		if n := len(out); n == 0 || math.Hypot(p[0]-out[n-1][0], p[1]-out[n-1][1]) >= sampleStepM {
			out = append(out, p)
		}
	}
	return out
}

// meanNearestNeighborDistance is the whole-path form of the detector's running score: for every point
// in a, the distance to its nearest point in b, averaged (+Inf if either is empty).
func meanNearestNeighborDistance(a, b [][2]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return math.Inf(1)
	}
	var sum float64
	for _, p := range a {
		sum += math.Sqrt(nearestSq(p, b))
	}
	return sum / float64(len(a))
}

// SampleCountForTest returns how many driven points sessionID has queued or scored for shape matching.
func (d *TrackDetector) SampleCountForTest(sessionID string) int {
	st := d.session(sessionID)
	if st == nil {
		return 0
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.samples + len(st.pending)
}

// SetEndTimeoutForTest shortens the session-end database timeout so a hang test runs quickly.
func SetEndTimeoutForTest(d *TrackDetector, timeout time.Duration) { d.endTimeout = timeout }

// SetGeomTimeoutForTest shortens the geometry-load deadline (geomLoadTimeout) so tests needn't wait 30 s.
func SetGeomTimeoutForTest(d *TrackDetector, timeout time.Duration) { d.geomTimeout = timeout }
