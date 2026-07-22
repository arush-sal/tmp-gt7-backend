package api

import (
	"math"
	"net/http"

	"github.com/arush-sal/garage/backend/rtps/rkbref"
	"go.uber.org/zap"
)

// trackCoverageDTO is one track's RKB-geometry readiness: whether it has a centerline, how many usable
// sector labels it carries, whether sector timing is ready (centerline + >= 2 labels), and whether it
// still needs a coordinate capture.
type trackCoverageDTO struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	HasCenterline    bool   `json:"has_centerline"`
	SectorLabelCount int    `json:"sector_label_count"`
	SectorReady      bool   `json:"sector_ready"`
	NeedsCapture     bool   `json:"needs_capture"`
}

// handleTracksCoverage: GET /v1/tracks/coverage -> per-track sector-timing readiness, in RKB index
// order. Reuses rkbref.SectorsFromRecord so "ready" here means exactly what the serving path treats as
// usable geometry (reason == "" with sectors), never a second, drifting definition.
func (s *Server) handleTracksCoverage(w http.ResponseWriter, r *http.Request) {
	if s.tracks == nil {
		writeError(w, http.StatusServiceUnavailable, "track RKB unavailable")
		return
	}
	ctx := r.Context()
	idx, err := s.tracks.Index(ctx)
	if err != nil {
		s.log.Error("rkb index failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list track coverage")
		return
	}

	out := make([]trackCoverageDTO, 0, len(idx))
	for _, e := range idx {
		dto := trackCoverageDTO{Slug: e.Slug, Name: e.Name, NeedsCapture: e.NeedsCapture}
		if rec, found, _ := s.tracks.Track(ctx, e.Slug); found {
			sectors, reason := rkbref.SectorsFromRecord(rec)
			dto.HasCenterline = reason != "no-centerline"
			dto.SectorReady = reason == "" && len(sectors) > 0
			for _, sec := range rec.Sectors {
				if lw := sec.LabelWorld; lw != nil && len(*lw) >= 2 && finite((*lw)[0]) && finite((*lw)[1]) {
					dto.SectorLabelCount++
				}
			}
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// finite reports whether v is neither NaN nor infinite (mirrors rkbref's label filter, unexported there).
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
