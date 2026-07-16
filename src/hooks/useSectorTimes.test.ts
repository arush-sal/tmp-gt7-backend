// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { useSectorTimes } from "./useSectorTimes";
import type { LiveSnapshot } from "../contract/live";
import type { DrawGeometry } from "../lib/rkbGeometry";

const geometry: DrawGeometry = {
  left: [], right: [], outline: [], widthM: null, corners: [], startFinish: null,
  cl: [[0, 0], [10, 0], [20, 0], [30, 0]],
  sectors: [
    { label: "S1", x: 0, z: 0 },
    { label: "S2", x: 10, z: 0 },
    { label: "S3", x: 20, z: 0 },
  ],
};

const enr = { analysis: null, car_name: null, track: "t", delta_to_best_ns: null, session_id: "sess-1" };

function live(lap: number, lapTimeNs: number, x: number, z: number): LiveSnapshot {
  return {
    served_at: "x",
    status: "live",
    frame: {
      car_speed: 1, current_gear: 1, suggested_gear: 1, rpm: 1, throttle: 0, brake: 0, clutch: 0,
      boost: 0, current_fuel: 1, fuel_capacity: 1, tyre_temp_fl: 1, tyre_temp_fr: 1, tyre_temp_rl: 1,
      tyre_temp_rr: 1, current_lap: lap, total_laps: 5, current_position: 1, total_positions: 1,
      best_lap: null, last_lap: null, in_race: true, is_paused: false,
      position_x: x, position_z: z,
    },
    derived: { current_lap_time_ns: lapTimeNs, fuel_pct: 1, source: "LIVE" },
    enrichment: enr,
  };
}

describe("useSectorTimes", () => {
  it("returns null when there is no geometry", () => {
    const { result } = renderHook(
      ({ snapshot }) => useSectorTimes(snapshot, null, null),
      { initialProps: { snapshot: live(1, 0, 0, 0) as LiveSnapshot | null } },
    );
    expect(result.current).toBeNull();
  });

  it("builds up sector splits across a sequence of snapshots for one track", () => {
    const { result, rerender } = renderHook(
      ({ snapshot }) => useSectorTimes(snapshot, geometry, "track-a"),
      { initialProps: { snapshot: live(1, 0, 0, 0) as LiveSnapshot | null } },
    );
    expect(result.current).toEqual([
      { sector: "S1", timeMs: null },
      { sector: "S2", timeMs: null },
      { sector: "S3", timeMs: null },
    ]);

    rerender({ snapshot: live(1, 2_000_000_000, 9, 0) }); // crosses into S2
    expect(result.current).toEqual([
      { sector: "S1", timeMs: 2000 },
      { sector: "S2", timeMs: null },
      { sector: "S3", timeMs: null },
    ]);
  });

  it("rebuilds the timer once geometry arrives after the initial null-geometry render (same trackKey)", () => {
    // Mirrors real startup: trackKey is set by auto-detect before useTrack's async
    // load resolves, so the first render(s) have geometry=null -> no partition.
    const { result, rerender } = renderHook(
      ({ snapshot, geom }) => useSectorTimes(snapshot, geom, "track-a"),
      {
        initialProps: {
          snapshot: live(1, 0, 0, 0) as LiveSnapshot | null,
          geom: null as DrawGeometry | null,
        },
      },
    );
    expect(result.current).toBeNull();

    // Geometry resolves; same trackKey, same snapshot lap/pos so far.
    rerender({ snapshot: live(1, 0, 0, 0), geom: geometry });
    expect(result.current).toEqual([
      { sector: "S1", timeMs: null },
      { sector: "S2", timeMs: null },
      { sector: "S3", timeMs: null },
    ]);

    // Sector splits now compute normally on the newly-partitioned timer.
    rerender({ snapshot: live(1, 2_000_000_000, 9, 0), geom: geometry });
    expect(result.current).toEqual([
      { sector: "S1", timeMs: 2000 },
      { sector: "S2", timeMs: null },
      { sector: "S3", timeMs: null },
    ]);
  });

  it("resets when trackKey changes (new track)", () => {
    const { result, rerender } = renderHook(
      ({ snapshot, key }) => useSectorTimes(snapshot, geometry, key),
      { initialProps: { snapshot: live(1, 0, 0, 0) as LiveSnapshot | null, key: "track-a" as string | null } },
    );
    rerender({ snapshot: live(1, 2_000_000_000, 9, 0), key: "track-a" });
    expect(result.current![0].timeMs).toBe(2000);

    rerender({ snapshot: live(1, 2_000_000_000, 9, 0), key: "track-b" });
    // fresh timer for track-b, fed the same snapshot fresh (lap treated as newly seen at S1 -> crossing into S2)
    expect(result.current).toEqual([
      { sector: "S1", timeMs: 2000 },
      { sector: "S2", timeMs: null },
      { sector: "S3", timeMs: null },
    ]);
  });
});
