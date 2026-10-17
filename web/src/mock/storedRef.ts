import type { StoredReferenceDTO, PredictedSampleDTO } from "../contract/session";

const sample = (fraction: number, t: number, steering = 0): PredictedSampleDTO => ({
  fraction, t_ns: t * 1e9, distance: fraction * 1000, pos_x: fraction, pos_z: 0, speed: 100 + fraction * 100,
  throttle: 0, brake: 0, rpm: 0, gear: 3, fuel: 50, tyre_temp_fl: 0, tyre_temp_fr: 0, tyre_temp_rl: 0, tyre_temp_rr: 0,
  clutch: 0, boost: 0, delta_to_best_ns: null, steering,
});

/** storedRef builds a StoredReferenceDTO fixture (test helper). */
export const storedRef = (over: Partial<StoredReferenceDTO> = {}): StoredReferenceDTO => ({
  reliable: true, reason: null, predicted_best_ns: 90685000000, sectors: [{ index: 0, label: "S1", owner_lap: 2, duration_ns: 30000000000 }],
  samples: [sample(0, 0), sample(0.5, 45, Math.PI), sample(1, 90.685)],
  source: "pinned", session_id: "sess-A", lap_number: 2, lap_time: "90.685s", recorded_at: "2026-07-01T10:00:00Z",
  car: { id: 123, name: "Test GT3", category: "Gr.3" },
  ...over,
});
