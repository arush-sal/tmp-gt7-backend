// @vitest-environment jsdom
// src/components/Dashboard.test.tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";
import Dashboard from "./Dashboard";
import type { LiveSource, LiveHandlers } from "../lib/liveSource";
import { validateLive } from "../lib/validateLive";
import { liveSample } from "../mock/liveSample";

// jsdom has no <canvas> 2D context, so react-konva's real DOM/canvas plumbing can't run
// here. Stub the handful of primitives TrackTraceCanvas uses with inert passthroughs —
// Dashboard only needs the tree to mount without crashing, not to inspect Konva output.
vi.mock("react-konva", () => ({
  Stage: ({ children }: { children?: unknown }) => <div>{children as never}</div>,
  Layer: ({ children }: { children?: unknown }) => <div>{children as never}</div>,
  Line: () => null,
  Circle: () => null,
  Text: () => null,
  Shape: () => null,
}));

// Dashboard fetches the track list (and useTrack fetches a track record) via the real
// `fetch` global — stub it for every test in this file so none of them hit the network.
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => [] })) as unknown as typeof fetch);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

// Dashboard opens two independent subscriptions on this same injected `source` (useSession
// for the header/panels, useHistory for the telemetry-graph buffer — deferred single-
// subscription fan-out is a later pass per the P3 plan). A real SSE endpoint fans the same
// events out to every listener, so the fake tracks every subscriber and broadcasts to all.
function fakeSource(): { source: LiveSource; push: () => void; pushIdle: () => void; fail: (m: string) => void } {
  const subscribers: LiveHandlers[] = [];
  const source: LiveSource = (_url, h) => { subscribers.push(h); return () => {}; };
  return {
    source,
    push: () => subscribers.forEach(h => h.onSnapshot(validateLive(liveSample))),
    pushIdle: () => subscribers.forEach(h => h.onSnapshot(validateLive({
      served_at: "2026-07-07T00:00:00Z",
      status: "idle",
      frame: null,
      derived: null,
      enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: null },
    }))),
    fail: (m: string) => subscribers.forEach(h => h.onError(new Error(m))),
  };
}

describe("Dashboard (live)", () => {
  it("shows a connection banner before the first snapshot", () => {
    const { source } = fakeSource();
    render(<Dashboard source={source} />);
    expect(screen.getByText(/connecting|waiting|no telemetry/i)).toBeTruthy();
  });

  it("renders live values after a snapshot arrives", () => {
    const { source, push } = fakeSource();
    render(<Dashboard source={source} />);
    act(() => push());
    expect(screen.getByText("P1/20")).toBeTruthy();                        // position (header only)
    expect(screen.getByText("77/90")).toBeTruthy();                        // lap (header only)
    expect(screen.getAllByText("1:08.602").length).toBeGreaterThan(0);     // best lap (header + lap history)
    expect(screen.getByText("100%")).toBeTruthy();                         // fuel (right panel only)
    expect(screen.getByText("87°C")).toBeTruthy();                         // hot tyre rl (right panel only)
    expect(screen.queryByText(/connecting|waiting|no telemetry/i)).toBeNull();
  });

  it("keeps live values and surfaces the error banner on a stream error", () => {
    const { source, push, fail } = fakeSource();
    render(<Dashboard source={source} />);
    act(() => push());
    act(() => fail("boom"));
    expect(screen.getByText("P1/20")).toBeTruthy();               // last-good retained
    // error surfaced over live data — both the connection banner and the track-trace
    // status line reflect it (per Task 12 wiring), so assert at least one match.
    expect(screen.getAllByText(/stream error: boom/i).length).toBeGreaterThan(0);
  });

  it("shows a waiting-for-telemetry banner when the backend is idle", () => {
    const { source, pushIdle } = fakeSource();
    render(<Dashboard source={source} />);
    act(() => pushIdle());
    // idle-specific copy (the connecting banner also contains "waiting for telemetry")
    expect(screen.getByText(/start a session in GT7/i)).toBeTruthy();
  });

  it("renders the trace canvas (no mock TrackMap) and hides the picker until a track is detected", async () => {
    function sourceOnce(): LiveSource {
      return (_url, h) => {
        const s = structuredClone(liveSample) as { frame: Record<string, unknown>; enrichment: Record<string, unknown> };
        s.frame.position_x = 10;
        s.frame.position_z = 20;
        s.enrichment.track = null; // no detected track
        h.onSnapshot(validateLive(s));
        return () => {};
      };
    }
    render(<Dashboard source={sourceOnce()} />);
    // Canvas always renders; the old mock TrackMap is gone.
    await waitFor(() => expect(screen.getByText("Track Trace")).toBeInTheDocument());
    expect(screen.queryByText("click any corner to inspect →")).not.toBeInTheDocument();
    // With no detected/selected track, the "select track" dropdown is not shown.
    expect(screen.queryByLabelText("Track")).not.toBeInTheDocument();
  });

  // Regression for the reviewed blocker: `geometry` used to be recomputed fresh on every
  // render (`track.record ? toDrawGeometry(...) : null`), which was a dep of the mismatch
  // `useEffect`'s `setInterval(1000)` — so the interval was torn down + re-armed every
  // render and its callback never actually fired. Memoizing `geometry` on `track.record`
  // fixes it.
  //
  // A single batched `act()` with 305 pushes does NOT reproduce this: React collapses the
  // whole batch into one render + one settle, so the interval only ever sees a single
  // "quiet" gap — it fires regardless of memoization. The real bug only shows up when the
  // component keeps RE-RENDERING while the 1s window is trying to accrue: each render, with
  // the inline (non-memoized) `geometry`, hands the effect a new object identity, tearing
  // down and re-arming the interval before it can reach 1000ms. So here each snapshot is
  // pushed in its OWN `act()` (one render per push, mirroring the ~60Hz production render
  // cadence) with a small timer advance interleaved between pushes — never a single
  // uninterrupted gap anywhere near 1000ms — so the memoized case must survive across all
  // those renders (interval keeps counting) while the unmemoized case never accrues past
  // ~50ms before being reset.
  it("surfaces the mismatch chip once >=minSamples off-geometry positions accrue and 1s elapses", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (String(url).includes("/v1/tracks/")) {
        return {
          ok: true,
          json: async () => ({
            boundary: { left: [[0, 0], [1, 0]], right: [[0, 1], [1, 1]], outline: [] },
          }),
        };
      }
      return { ok: true, json: async () => [{ slug: "test-track", name: "Test Track" }] };
    }) as unknown as typeof fetch);

    const subscribers: LiveHandlers[] = [];
    const source: LiveSource = (_url, h) => { subscribers.push(h); return () => {}; };

    // Fake timers must be installed BEFORE the mismatch effect's setInterval is created —
    // vi.useFakeTimers() does not retroactively adopt a setInterval already scheduled on
    // the real clock, so it has to be armed up front. Promise microtasks (fetch flushing
    // below) are unaffected by fake timers and still resolve normally.
    vi.useFakeTimers();
    try {
      render(<Dashboard source={source} />);

      // Flush the initial /v1/tracks list fetch.
      await act(async () => { await Promise.resolve(); await Promise.resolve(); });

      const base = structuredClone(liveSample) as {
        frame: Record<string, unknown>; derived: Record<string, unknown>; enrichment: Record<string, unknown>;
      };
      base.enrichment.track = "test-track";
      base.enrichment.session_id = "sess-mismatch";
      base.frame.current_lap = 5;
      base.frame.car_speed = 100;

      const push = (i: number) => {
        const s = structuredClone(base);
        s.frame.position_x = 1000 + i * 0.5; // far outside the ~[0,1]x[0,1] bbox above
        s.frame.position_z = 1000 + i * 0.5;
        s.derived.current_lap_time_ns = 1_000_000_000 + i * 1_000_000;
        const snap = validateLive(s);
        subscribers.forEach(h => h.onSnapshot(snap));
      };

      // First push auto-detects the track (enrichment.track -> "test-track"), which triggers
      // useTrack's fetch of the record; flush that so `geometry` becomes non-null (and, in
      // the memoized case, the mismatch interval gets armed) before the render-churn loop.
      act(() => push(0));
      await act(async () => {
        for (let i = 0; i < 5; i++) await Promise.resolve();
      });

      // Each iteration advances the fake clock by 15ms, so (in the memoized case, where
      // the interval survives every render) it fires roughly every 66-67 iterations
      // (1000ms / 15ms). isMismatch also requires >= minSamples (300) moving samples, so
      // the loop must run well past the iteration where the *next* firing lands at or
      // after 300 accrued samples (that firing is around iteration ~333) — 400 gives
      // comfortable margin for a firing (~iteration 400, ~399 moving samples) to land
      // after the 300 threshold, while still keeping each individual advance (15ms) far
      // short of the 1000ms an unmemoized (repeatedly reset) interval would need.
      const N = 400;
      for (let i = 1; i < N; i++) {
        // One push == one render (own `act()`), then a small advance — never close to
        // the 1000ms the interval needs in one uninterrupted gap. Deliberately no big
        // quiet gap is left AFTER the loop either (see below): a long trailing advance
        // would let even a repeatedly-reset interval fire on that last gap alone, which
        // would defeat the whole point of this churn and pass regardless of memoization.
        act(() => push(i));
        act(() => { vi.advanceTimersByTime(15); });
      }

      // Assert immediately, with no further (quiet) time advance: the memoized interval
      // has already fired multiple times as fake-time accrued across the many renders
      // above, the last of those firings past the >= 300 moving-sample threshold; the
      // unmemoized interval has never seen an uninterrupted gap wider than ~15ms and so
      // must never have fired at all.
      expect(screen.getByText("mismatch suspected")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  // Race lifecycle (task E1w): the history store only grows while `live`, and wipes on a
  // genuine new race — both driven by Dashboard's effects off useSession's `live`/`newRaceSeq`.
  // Probed through the lap-selector's "Lap N" options (fed by history.completedLapCount()),
  // since Dashboard doesn't expose the store directly.
  it("gates history growth on `live` and resets it on a new race", () => {
    const subscribers: LiveHandlers[] = [];
    const source: LiveSource = (_url, h) => { subscribers.push(h); return () => {}; };
    const push = (overrides: {
      session: string; lap: number; totalLaps?: number; t: number;
    }) => {
      const s = structuredClone(liveSample) as {
        frame: Record<string, unknown>; derived: Record<string, unknown>; enrichment: Record<string, unknown>;
      };
      s.frame.current_lap = overrides.lap;
      s.frame.total_laps = overrides.totalLaps ?? 90;
      s.derived.current_lap_time_ns = overrides.t;
      s.enrichment.session_id = overrides.session;
      const snap = validateLive(s);
      subscribers.forEach(h => h.onSnapshot(snap));
    };
    const lapOptions = () =>
      screen.getAllByRole("option").map(o => o.textContent).filter(t => /^Lap \d+$/.test(t ?? ""));

    render(<Dashboard source={source} />);

    // `history.setLive` runs once on mount with useSession's initial `live` (false), so the
    // very first snapshot always finds the store gated shut and is dropped before the effect
    // catches up to `live=true` — a priming push before the real assertions absorbs that.
    act(() => push({ session: "a", lap: 1, t: 1_000_000_000 }));

    // Laps 2..4: each lap change completes the previous lap's buffer.
    act(() => push({ session: "a", lap: 2, t: 2_000_000_000 }));
    act(() => push({ session: "a", lap: 3, t: 3_000_000_000 }));
    act(() => push({ session: "a", lap: 4, t: 4_000_000_000 }));
    expect(lapOptions()).toEqual(["Lap 2", "Lap 1"]);

    // Race-over frame (current_lap > total_laps -> live=false) then another push while
    // still not-live: the second push must be gated (no further growth).
    act(() => push({ session: "a", lap: 11, totalLaps: 10, t: 5_000_000_000 }));
    const afterRaceOver = lapOptions().length;
    act(() => push({ session: "a", lap: 11, totalLaps: 10, t: 6_000_000_000 }));
    expect(lapOptions().length).toBe(afterRaceOver);

    // New race (session "b", lap 1): newRaceSeq bumps -> history.reset() wipes the store.
    // The first post-reset push is itself dropped (same stale-gate reason as above), so two
    // more pushes are needed to prove growth restarted clean, not still holding session "a".
    act(() => push({ session: "b", lap: 1, t: 100_000_000 }));
    act(() => push({ session: "b", lap: 2, t: 200_000_000 }));
    act(() => push({ session: "b", lap: 3, t: 300_000_000 }));
    expect(lapOptions()).toEqual(["Lap 1"]);
  });

  // Task G: the detected/selected track slug is persisted (gt7:track) and seeded back on
  // mount, so a refresh restores the band/sectors even before (or without) the feed
  // re-detecting the track.
  it("restores the track band from a persisted slug on refresh, without the feed re-detecting", async () => {
    localStorage.setItem("gt7:track", JSON.stringify({ schemaVersion: 1, payload: "test-track" }));
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (String(url).includes("/v1/tracks/")) {
        return {
          ok: true,
          json: async () => ({
            boundary: { left: [[0, 0], [1, 0]], right: [[0, 1], [1, 1]], outline: [] },
          }),
        };
      }
      return { ok: true, json: async () => [{ slug: "test-track", name: "Test Track" }] };
    }) as unknown as typeof fetch);

    // Feed not detecting (enrichment.track null) -- the seeded slug must still hold.
    const source: LiveSource = (_url, h) => {
      const s = structuredClone(liveSample) as { enrichment: Record<string, unknown> };
      s.enrichment.track = null;
      h.onSnapshot(validateLive(s));
      return () => {};
    };
    render(<Dashboard source={source} />);
    await waitFor(() => expect(screen.getByLabelText("Track")).toBeInTheDocument());
    // Assert the RESTORED slug specifically (not just that the picker rendered): the seed came from
    // storage, since the pushed frame's enrichment.track was null (feed not detecting).
    expect(screen.getByLabelText<HTMLSelectElement>("Track").value).toBe("test-track");
  });
});
