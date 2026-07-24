-- +goose Up
-- +goose StatementBegin
-- Per-wheel surface-material code (GT7 format-C only): 4 chars, order FL,FR,RL,RR
-- (T=tarmac C=kerb D=dirt G=grass S=sand s=snow). Used to invalidate a sector when all four tyres are
-- off-track (see rtps/processing/sector.go allTyresOff). NOT NULL DEFAULT '' so pre-existing rows and
-- format-A frames backfill to empty — an empty code counts as "no data", never as off-track.
ALTER TABLE lap_trace
    ADD COLUMN surface_type TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE lap_trace
    DROP COLUMN surface_type;
-- +goose StatementEnd
