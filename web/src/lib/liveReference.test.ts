import { describe, it, expect } from "vitest";
import { createLiveRefAligner } from "./liveReference";
import { storedRef } from "../mock/storedRef";
import type { TracePoint } from "./historyStore";

// storedRef: pos_x = fraction (0, .5, 1), pos_z = 0, speed = 100 + 100*fraction, t = 0, 45, 90.685 s.
const pt = (posX: number, t?: number) => ({ posX, posZ: 0, throttle: 0, brake: 0, surface: "", t }) as TracePoint;
const ref = () => storedRef();

describe("createLiveRefAligner", () => {
  it("aligns live samples by nearest reference position", () => {
    const col = createLiveRefAligner(ref()).column("speed", [pt(0), pt(0.5), pt(1)])!;
    expect(col[0]).toBeCloseTo(100); expect(col[1]).toBeCloseTo(150); expect(col[2]).toBeCloseTo(200);
  });
  it("falls back to elapsed lap time (ghost) when the position is unusable", () => {
    const col = createLiveRefAligner(ref()).column("speed", [pt(NaN, 45)])!;
    expect(col[0]).toBeCloseTo(150); // t=45s is fraction 0.5
  });
  it("extends incrementally and recomputes when the live buffer restarts", () => {
    const a = createLiveRefAligner(ref());
    const first = [pt(0), pt(0.5)];
    expect(a.column("speed", first)!).toHaveLength(2);
    first.push(pt(1));
    expect(a.column("speed", first)![2]).toBeCloseTo(200);
    const next = [pt(1)]; // new lap: shorter, different buffer
    expect(a.column("speed", next)![0]).toBeCloseTo(200);
  });
  it("returns null for channels the reference lacks (tyres)", () => {
    expect(createLiveRefAligner(ref()).column("tyreFL", [pt(0)])).toBeNull();
  });
  it("converts steering radians to degrees like the lap overlay", () => {
    const col = createLiveRefAligner(ref()).column("steering", [pt(0.5)]);
    expect(col![0]).toBeCloseTo(180);
  });
});
