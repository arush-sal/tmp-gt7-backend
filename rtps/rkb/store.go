package rkb

import "context"

// Store serves the Reference-Knowledge Track RKB. Index/Track are read-only; the interface is the
// seam for a future Postgres backing (D-B5 style) without touching the API.
type Store interface {
	// Index returns one entry per track, in slug order.
	Index(ctx context.Context) ([]IndexEntry, error)
	// Track returns the assembled canonical record for slug; found is false for an unknown slug.
	Track(ctx context.Context, slug string) (rec *Record, found bool, err error)
	// ReferenceTime returns the reference (fastest) time in ms for a (track, sector, car, PP) setup;
	// found is false when no reference row exists. sector "" means the whole-track reference.
	// (Table is empty in v1; wired for the future Analysis Engine.)
	ReferenceTime(ctx context.Context, slug, sector string, carID int, pp float64) (ms int, found bool, err error)
	// Close releases the underlying handle.
	Close() error
}
