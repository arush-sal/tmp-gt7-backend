-- +goose Up
-- +goose StatementBegin
-- P2-1: extra telemetry channels threaded into the served per-lap trace. fuel/tyre temps/clutch/boost
-- are NOT NULL DEFAULT 0 to backfill any pre-existing rows (a 0 reading is a valid absence, matching
-- the recorder's fuelPct guard). delta_to_best_ns is the live delta-to-best at each frame and is
-- nullable: NULL means no delta was available (no reference lap yet / DeltaFn unset), distinct from 0.
ALTER TABLE lap_trace
    ADD COLUMN fuel             REAL   NOT NULL DEFAULT 0, -- percent 0-100 of tank capacity
    ADD COLUMN tyre_temp_fl     REAL   NOT NULL DEFAULT 0, -- degrees C, front-left
    ADD COLUMN tyre_temp_fr     REAL   NOT NULL DEFAULT 0, -- degrees C, front-right
    ADD COLUMN tyre_temp_rl     REAL   NOT NULL DEFAULT 0, -- degrees C, rear-left
    ADD COLUMN tyre_temp_rr     REAL   NOT NULL DEFAULT 0, -- degrees C, rear-right
    ADD COLUMN clutch           REAL   NOT NULL DEFAULT 0, -- clutch engagement 0-1
    ADD COLUMN boost            REAL   NOT NULL DEFAULT 0, -- turbo boost pressure
    ADD COLUMN delta_to_best_ns BIGINT;                    -- live delta-to-best (ns), NULL when unavailable
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE lap_trace
    DROP COLUMN fuel,
    DROP COLUMN tyre_temp_fl,
    DROP COLUMN tyre_temp_fr,
    DROP COLUMN tyre_temp_rl,
    DROP COLUMN tyre_temp_rr,
    DROP COLUMN clutch,
    DROP COLUMN boost,
    DROP COLUMN delta_to_best_ns;
-- +goose StatementEnd
