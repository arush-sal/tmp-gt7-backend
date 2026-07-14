package processing

import (
	"math"

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
// subsample stride and bbox tolerance as matchDecision, returning per-candidate diagnostics sorted by
// score ascending. Test-only: lets the tuning test see the correct track's rank/score versus the best
// wrong candidate, which is what the thresholds must separate.
func CandidateDiagnosticsForTest(points [][2]float64, geoms []rkb.TrackGeom) []CandidateDiag {
	drivenBbox := computeBbox(points)
	sampled := subsamplePoints(points, drivenStride)
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

// ForwardReverseTwinForTest exposes forwardReverseTwin to tests, taking plain fields instead of the
// unexported candidateScore type (used by the real-RKB-corpus twin test, which cannot construct a
// candidateScore directly from out-of-package processing_test).
func ForwardReverseTwinForTest(
	aSlug string, aReverse bool, aBase int, aPoints [][2]float64,
	bSlug string, bReverse bool, bBase int, bPoints [][2]float64,
) (string, bool) {
	a := candidateScore{slug: aSlug, isReverse: aReverse, base: aBase, points: aPoints}
	b := candidateScore{slug: bSlug, isReverse: bReverse, base: bBase, points: bPoints}
	twin, ok := forwardReverseTwin(a, b)
	return twin.slug, ok
}
