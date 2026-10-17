-- +goose Up
-- +goose StatementBegin
-- Lap fingerprint: a hash identifying a completed timed lap across sessions (see
-- session.LapFingerprint). GT7 in-game replays are content-identical copies of earlier frames, so a
-- replayed lap hashes the same as the stored original and the replay session is dropped as a duplicate.
-- NULL for the formation lap, partial laps and pre-migration rows.
ALTER TABLE laps
    ADD COLUMN fingerprint TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- One stored lap per fingerprint. A plain UNIQUE index allows any number of NULLs (no-fingerprint laps).
CREATE UNIQUE INDEX IF NOT EXISTS laps_fingerprint_key ON laps (fingerprint);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS laps_fingerprint_key;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE laps
    DROP COLUMN fingerprint;
-- +goose StatementEnd
