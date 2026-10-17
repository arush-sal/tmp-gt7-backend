import { drawTrace } from "./drawTrace";

test("drawTrace strokes the current lap path", () => {
  const calls: string[] = [];
  const ctx = new Proxy({}, {
    get: (_t, p) => typeof p === "string" && ["beginPath","moveTo","lineTo","stroke","save","restore"].includes(p)
      ? (..._a: any[]) => { calls.push(p); } : () => {},
    set: () => true,
  }) as any;
  drawTrace(ctx, {
    trace: { current: [
      { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 20, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
    ] },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
  });
  expect(calls).toContain("beginPath");
  expect(calls).toContain("lineTo");
  expect(calls).toContain("stroke");
});

test("drawTrace uses a thin racing line", () => {
  // record each stroke pass along with the strokeStyle/lineWidth active at stroke() time
  const strokes: Array<{ style: unknown; width: unknown }> = [];
  const state: { strokeStyle: unknown; lineWidth: unknown } = { strokeStyle: undefined, lineWidth: undefined };
  const ctx = new Proxy({}, {
    get: (_t, p) => {
      if (p === "strokeStyle") return state.strokeStyle;
      if (p === "lineWidth") return state.lineWidth;
      if (p === "stroke") return () => { strokes.push({ style: state.strokeStyle, width: state.lineWidth }); };
      if (typeof p === "string" && ["beginPath", "moveTo", "lineTo", "save", "restore"].includes(p)) {
        return (..._a: any[]) => {};
      }
      return () => {};
    },
    set: (_t, p, v) => {
      if (p === "strokeStyle") state.strokeStyle = v;
      if (p === "lineWidth") state.lineWidth = v;
      return true;
    },
  }) as any;

  drawTrace(ctx, {
    trace: { current: [
      { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 10, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
      { posX: 20, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
      { posX: 30, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    ] },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
  });

  // no wide translucent brake-zone-region pass remains
  const brakeZoneStrokes = strokes.filter(s =>
    typeof s.width === "number" && (s.width as number) >= 10 &&
    typeof s.style === "string" && (s.style as string).includes("225,6,0"));
  expect(brakeZoneStrokes.length).toBe(0);

  const normalStrokes = strokes.filter(s => s.width === 3 &&
    typeof s.style === "string" && !(s.style as string).includes("225,6,0"));
  expect(normalStrokes.length).toBeGreaterThan(0);
});

test("brake-zone sawtooth ribbon is confined to actual brake spans, not scattered light-brake segments", () => {
  // record each stroke pass along with the strokeStyle/lineWidth active at stroke() time.
  const strokes: Array<{ style: unknown; width: unknown }> = [];
  const state: { strokeStyle: unknown; lineWidth: unknown } = { strokeStyle: undefined, lineWidth: undefined };
  const ctx = new Proxy({}, {
    get: (_t, p) => {
      if (p === "strokeStyle") return state.strokeStyle;
      if (p === "lineWidth") return state.lineWidth;
      if (p === "stroke") return () => { strokes.push({ style: state.strokeStyle, width: state.lineWidth }); };
      if (typeof p === "string" && ["beginPath", "moveTo", "lineTo", "save", "restore"].includes(p)) {
        return (..._a: any[]) => {};
      }
      return () => {};
    },
    set: (_t, p, v) => {
      if (p === "strokeStyle") state.strokeStyle = v;
      if (p === "lineWidth") state.lineWidth = v;
      return true;
    },
  }) as any;

  // a short braking burst (idx 2-4) surrounded by throttle-only segments (idx 0-1, 5-6);
  // brake never dips into the 12-20 "light brake" band that used to smear the zigzag
  // across the rest of the lap.
  drawTrace(ctx, {
    trace: { current: [
      { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 5, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 10, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
      { posX: 15, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
      { posX: 20, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
      { posX: 25, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 30, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    ] },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
  });

  // the sawtooth pass is a distinct (width 2.2) near-solid red stroke, separate from the
  // normal racing-line strokes (width 3, a throttle/brake colour, never this exact red).
  const sawtoothStrokes = strokes.filter(s =>
    s.width === 2.2 && typeof s.style === "string" && (s.style as string) === "rgba(225,6,0,0.9)");
  expect(sawtoothStrokes.length).toBeGreaterThan(0);
});

test("brake-zone sawtooth ribbon does not appear on an all-throttle lap", () => {
  const strokes: Array<{ style: unknown; width: unknown }> = [];
  const state: { strokeStyle: unknown; lineWidth: unknown } = { strokeStyle: undefined, lineWidth: undefined };
  const ctx = new Proxy({}, {
    get: (_t, p) => {
      if (p === "strokeStyle") return state.strokeStyle;
      if (p === "lineWidth") return state.lineWidth;
      if (p === "stroke") return () => { strokes.push({ style: state.strokeStyle, width: state.lineWidth }); };
      if (typeof p === "string" && ["beginPath", "moveTo", "lineTo", "save", "restore"].includes(p)) {
        return (..._a: any[]) => {};
      }
      return () => {};
    },
    set: (_t, p, v) => {
      if (p === "strokeStyle") state.strokeStyle = v;
      if (p === "lineWidth") state.lineWidth = v;
      return true;
    },
  }) as any;

  drawTrace(ctx, {
    trace: { current: [
      { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 5, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 15, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    ] },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
  });

  const sawtoothStrokes = strokes.filter(s =>
    typeof s.style === "string" && (s.style as string) === "rgba(225,6,0,0.9)");
  expect(sawtoothStrokes.length).toBe(0);
});

test("select=\"current\" draws the current lap, same as \"all\"", () => {
  const lap = [
    { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 20, posZ: 0, throttle: 0, brake: 80, surface: "TTTT" },
  ];
  function countStrokes(select: "all" | "current" | number): number {
    const calls: string[] = [];
    const ctx = new Proxy({}, {
      get: (_t, p) => typeof p === "string" && ["beginPath", "moveTo", "lineTo", "stroke", "save", "restore"].includes(p)
        ? (..._a: any[]) => { calls.push(p); } : () => {},
      set: () => true,
    }) as any;
    drawTrace(ctx, {
      trace: { current: lap },
      view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
      select,
    });
    return calls.filter(c => c === "stroke").length;
  }

  const allStrokes = countStrokes("all");
  const currentStrokes = countStrokes("current");
  // completed laps are no longer retained; both "all" and "current" draw the current lap only.
  expect(currentStrokes).toBe(allStrokes);
  expect(currentStrokes).toBeGreaterThan(0);
});

test("a numeric select draws the passed analysisLap", () => {
  const analysisLap = [
    { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
  ];
  const current = [
    { posX: 0, posZ: 10, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 10, throttle: 100, brake: 0, surface: "TTTT" },
  ];
  const moveTos: Array<{ x: number; y: number }> = [];
  const ctx = new Proxy({}, {
    get: (_t, p) => {
      if (p === "moveTo") return (x: number, y: number) => { moveTos.push({ x, y }); };
      if (typeof p === "string" && ["beginPath", "lineTo", "stroke", "save", "restore"].includes(p)) {
        return (..._a: any[]) => {};
      }
      return () => {};
    },
    set: () => true,
  }) as any;

  drawTrace(ctx, {
    trace: { current },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
    select: 3,
    analysisLap,
  });

  // only analysisLap's points (posZ 0 -> screen y 0) should appear, never `current` (posZ 10).
  expect(moveTos.length).toBeGreaterThan(0);
  expect(moveTos.every(p => p.y === 0)).toBe(true);
});

test("a numeric select with no analysisLap draws nothing", () => {
  const calls: string[] = [];
  const ctx = new Proxy({}, {
    get: (_t, p) => typeof p === "string" && ["beginPath", "moveTo", "lineTo", "stroke", "save", "restore"].includes(p)
      ? (..._a: any[]) => { calls.push(p); } : () => {},
    set: () => true,
  }) as any;
  drawTrace(ctx, {
    trace: { current: [
      { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
      { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    ] },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
    select: 2,
  });
  expect(calls.length).toBe(0);
});
