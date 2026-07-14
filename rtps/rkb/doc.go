// Package rkb is the Reference-Knowledge serving layer. It reads the offline-generated, read-only
// Track RKB DB and assembles the canonical per-track record (world-metre geometry, corners,
// sectors, boundary, width, confidence, provenance). It is geometry-aware by design (unlike the API
// package); consumers depend only on Store, so the SQLite backing can be swapped for Postgres later.
package rkb
