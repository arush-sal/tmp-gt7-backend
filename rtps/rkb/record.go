package rkb

import "encoding/json"

// IndexEntry is one row of GET /v1/tracks - enough for a track picker + tier badges.
type IndexEntry struct {
	Slug                     string   `json:"slug"`
	Name                     string   `json:"name"`
	DDMID                    *int     `json:"ddm_id"`
	HexID                    *string  `json:"hex_id"`
	LengthM                  *float64 `json:"length_m"`
	NumCornersExpected       *int     `json:"num_corners_expected"`
	CoordinateUsability      string   `json:"coordinate_usability"`
	PhysicalBoundaryFidelity string   `json:"physical_boundary_fidelity"`
	NeedsCapture             bool     `json:"needs_capture"` // coordinate_usability == "needs-capture"
}

// Record is the full canonical per-track RKB record. Its JSON marshalling must be deep-equal to the
// source <slug>.rkb.json (see roundtrip_test.go).
type Record struct {
	Slug               string     `json:"slug"`
	Name               string     `json:"name"`
	DDMID              *int       `json:"ddm_id"`
	HexID              *string    `json:"hex_id"`
	Base               *int       `json:"base"`
	IsReverse          *bool      `json:"is_reverse"`
	IsOval             *bool      `json:"is_oval"`
	LengthM            *float64   `json:"length_m"`
	NumCornersExpected *int       `json:"num_corners_expected"`
	Affine             Affine     `json:"affine"`
	Corners            []Corner   `json:"corners"`
	Sectors            []Sector   `json:"sectors"`
	Boundary           Boundary   `json:"boundary"`
	Width              Width      `json:"width"`
	Confidence         Confidence `json:"confidence"`
	Provenance         Provenance `json:"provenance"`
}

// Affine is the world->SVG-px transform + fit metadata. bbox fields reassemble the 4 scalar columns
// into a [minx,minz,maxx,maxz] / [minx,miny,maxx,maxy] array, or null when unresolved.
type Affine struct {
	Scale          *float64   `json:"scale"`
	RotDeg         *float64   `json:"rot_deg"`
	Tx             *float64   `json:"tx"`
	Tz             *float64   `json:"tz"`
	Source         string     `json:"source"`
	RmsM           *float64   `json:"rms_m"`
	SfAnchorErrM   *float64   `json:"sf_anchor_err_m"`
	ScaleSanityPct *float64   `json:"scale_sanity_pct"`
	WorldBbox      *[]float64 `json:"world_bbox"`
	SvgBbox        *[]float64 `json:"svg_bbox"`
}

// Corner mirrors one corners row + its corner_phases. The sparse kinematic fields (apex_speed_kmh
// .. phases) are omitempty - absent on outline-derived corners.
type Corner struct {
	Turn             int        `json:"turn"`
	Source           string     `json:"source"`
	ApexWorld        []*float64 `json:"apex_world"` // [apex_x, apex_z]; coords may be null (212 corners)
	RadiusM          *float64   `json:"radius_m"`
	Direction        *string    `json:"direction"`
	Type             *string    `json:"type"`
	HeadingChangeDeg *float64   `json:"heading_change_deg"` // col corner_angle_deg
	ApexIsLabel      *bool      `json:"apex_is_label,omitempty"`
	ApexSpeedKmh     *float64   `json:"apex_speed_kmh,omitempty"`
	LatAccelG        *float64   `json:"lat_accel_g,omitempty"`
	GearAtApex       *int       `json:"gear_at_apex,omitempty"`
	Banked           *bool      `json:"banked,omitempty"`
	RollDegAtApex    *float64   `json:"roll_deg_at_apex,omitempty"` // col banking_deg_at_apex
	LengthM          *float64   `json:"length_m,omitempty"`
	// Phases is 3-way (see fidelity rule 1): absent (nil -> omitted) for stored/outline corners,
	// json.RawMessage("null") for source='derived-reverse', or the marshalled Phases object for
	// source='kinematic'. A *Phases + omitempty cannot express all three, hence RawMessage.
	Phases json.RawMessage `json:"phases,omitempty"`
}

// Phases holds the 4 corner-phase world points (each [x,z]); it is marshalled into Corner.Phases for
// kinematic corners. All four keys are emitted (kinematic always has turn_in/apex/exit; braking_zone
// is null when no braking was detected), so the fields are non-omitempty pointers. The outer pointer
// is nil when the phase has no corner_phases row at all; the inner []*float64 elements (consistent
// with Corner.ApexWorld) are individually nullable so a NULL world_x/world_z coordinate degrades to a
// null array element instead of forcing the whole point to null.
type Phases struct {
	BrakingZone *[]*float64 `json:"braking_zone"`
	TurnIn      *[]*float64 `json:"turn_in"`
	Apex        *[]*float64 `json:"apex"`
	Exit        *[]*float64 `json:"exit"`
}

// Sector is one sector: its member turns and (optional) label position.
type Sector struct {
	Sector     string     `json:"sector"`
	Turns      []int      `json:"turns"`       // parsed from the comma-string column
	LabelWorld *[]float64 `json:"label_world"` // [label_x, label_z] or null
}

// Boundary is the track boundary in world metres. SVG tracks (source='outline') carry outline; measured
// tracks (source='victorydash') carry left/right/cl (with outline empty). Reconstructed from
// boundary_points by side; per-side empty/absent must match source (finalized by the Task 6 gate).
type Boundary struct {
	Source string `json:"source"`
	// All four sides are omitempty and reconstructed only from the boundary_points rows that exist:
	// SVG tracks (source='outline') carry outline only; measured tracks (source='victorydash') carry
	// left/right/cl only. The absent side must be OMITTED (not emitted as null or []) to match source
	// (verified by the Task 6 round-trip gate across all 121 tracks - 0 tracks store an empty side).
	// Each point is [x,z]; a coord may be null (some SVG outlines carry [null,null] placeholder
	// vertices), so points are [][]*float64, not [][]float64.
	Outline [][]*float64 `json:"outline,omitempty"` // [[x,z],...]
	Left    [][]*float64 `json:"left,omitempty"`
	Right   [][]*float64 `json:"right,omitempty"`
	Cl      [][]*float64 `json:"cl,omitempty"` // centerline (measured tracks)
}

// Width is a single constant scalar for the whole track (per the base-width pass in the
// generator) plus its measurement source. There is no per-distance profile or min/max range.
type Width struct {
	WidthM *float64 `json:"width_m"`
	Source string   `json:"source"`
}

// Confidence carries the two independent tier dims + the join label.
type Confidence struct {
	CoordinateUsability      string `json:"coordinate_usability"`
	PhysicalBoundaryFidelity string `json:"physical_boundary_fidelity"`
	Join                     string `json:"join"`
}

// Provenance records how each layer was derived (method strings).
type Provenance struct {
	Affine   *string `json:"affine"`
	Boundary *string `json:"boundary"`
	Corners  *string `json:"corners"`
	Width    *string `json:"width"`
}
