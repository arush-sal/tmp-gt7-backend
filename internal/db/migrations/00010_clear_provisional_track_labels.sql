-- +goose Up
-- +goose StatementBegin
-- One-time cleanup: a lap's 'provisional' track label is the detector's unconfirmed guess, often wrong
-- (an unresolved track guessed as a neighbouring layout). From now on the label is cleared when its
-- session ends (see processing.TrackRefStore.ClearProvisionalLabels); this clears the rows from before.
-- Safe because migrations run at rtps startup, before any session is active in this process. A crash
-- can leave a session row 'active', but that session cannot resume: a new stream gets a new detector
-- and a new session, so no lock will ever confirm those laps and every provisional label at this moment
-- is final. Confident labels are untouched.
UPDATE laps SET track_slug = NULL, track_confidence = NULL WHERE track_confidence = 'provisional';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the cleared labels were unconfirmed guesses and cannot be reconstructed.
SELECT 1;
-- +goose StatementEnd
