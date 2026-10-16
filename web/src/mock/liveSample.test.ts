import { describe, it, expect } from "vitest";
import { validateLive } from "../lib/validateLive";
import { liveSample } from "./liveSample";

describe("liveSample", () => {
  it("is a valid live snapshot (passes validateLive)", () => {
    const s = validateLive(liveSample);
    expect(s.status).toBe("live");
    expect(s.frame!.current_lap).toBe(77);
    expect(s.frame!.tyre_temp_rl).toBeCloseTo(86.77724);
    expect(s.enrichment.delta_to_best_ns).toBe(61555907);
  });
});
