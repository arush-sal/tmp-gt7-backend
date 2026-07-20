import { drawTrace } from "./drawTrace";

test("drawTrace strokes the current lap path", () => {
  const calls: string[] = [];
  const ctx = new Proxy({}, {
    get: (_t, p) => typeof p === "string" && ["beginPath","moveTo","lineTo","stroke","save","restore"].includes(p)
      ? (..._a: any[]) => { calls.push(p); } : () => {},
    set: () => true,
  }) as any;
  drawTrace(ctx, {
    trace: { completed: [], current: [
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

test("drawTrace uses a thin racing line so the grey track band reads as the wide surface", () => {
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
    trace: { completed: [], current: [
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
    trace: { completed: [], current: [
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
    trace: { completed: [], current: [
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

test("select=\"current\" draws only the current lap, not completed laps", () => {
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
      trace: { completed: [lap, lap], current: lap },
      view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
      select,
    });
    return calls.filter(c => c === "stroke").length;
  }

  const allStrokes = countStrokes("all");
  const currentStrokes = countStrokes("current");
  expect(currentStrokes).toBeLessThan(allStrokes);
  expect(currentStrokes).toBeGreaterThan(0);
});

test("select=0 draws only completed[0]", () => {
  const lapA = [
    { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
  ];
  const lapB = [
    { posX: 0, posZ: 5, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 5, throttle: 100, brake: 0, surface: "TTTT" },
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
    trace: { completed: [lapA, lapB], current },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
    select: 0,
  });

  // only lapA's points should ever appear as a moveTo origin (posZ 0 -> screen y 0)
  expect(moveTos.length).toBeGreaterThan(0);
  expect(moveTos.every(p => p.y === 0)).toBe(true);
});

test("select out of range draws nothing", () => {
  const lap = [
    { posX: 0, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
    { posX: 10, posZ: 0, throttle: 100, brake: 0, surface: "TTTT" },
  ];
  const calls: string[] = [];
  const ctx = new Proxy({}, {
    get: (_t, p) => typeof p === "string" && ["beginPath", "moveTo", "lineTo", "stroke", "save", "restore"].includes(p)
      ? (..._a: any[]) => { calls.push(p); } : () => {},
    set: () => true,
  }) as any;
  drawTrace(ctx, {
    trace: { completed: [lap], current: lap },
    view: { scale: 1, offsetX: 0, offsetZ: 0 }, theme: "dark",
    select: 5,
  });
  expect(calls.length).toBe(0);
});
