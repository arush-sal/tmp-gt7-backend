import { describe, it, expect, vi } from "vitest";
import { subscribeLive, DEFAULT_STREAM_URL } from "./liveSource";

class FakeEventSource {
  static last: FakeEventSource | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor(public url: string) { FakeEventSource.last = this; }
  close() { this.closed = true; }
}

function withFakeES(fn: () => void) {
  const g = globalThis as any;
  const prev = g.EventSource;
  g.EventSource = FakeEventSource as any;
  try { fn(); } finally { g.EventSource = prev; }
}

const validBody = JSON.stringify({
  served_at: "2026-07-07T00:00:00Z", status: "idle", frame: null, derived: null,
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: null },
});

describe("subscribeLive", () => {
  it("opens the given url and forwards a validated snapshot", () => withFakeES(() => {
    const onSnapshot = vi.fn(), onError = vi.fn();
    subscribeLive("http://x/stream", { onSnapshot, onError });
    const es = FakeEventSource.last!;
    expect(es.url).toBe("http://x/stream");
    es.onmessage!({ data: validBody });
    expect(onSnapshot).toHaveBeenCalledOnce();
    expect(onError).not.toHaveBeenCalled();
  }));

  it("routes a malformed payload to onError, not onSnapshot", () => withFakeES(() => {
    const onSnapshot = vi.fn(), onError = vi.fn();
    subscribeLive("http://x/stream", { onSnapshot, onError });
    FakeEventSource.last!.onmessage!({ data: "{ not json" });
    expect(onSnapshot).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledOnce();
  }));

  it("routes a connection error to onError", () => withFakeES(() => {
    const onError = vi.fn();
    subscribeLive("http://x/stream", { onSnapshot: vi.fn(), onError });
    FakeEventSource.last!.onerror!();
    expect(onError).toHaveBeenCalledOnce();
  }));

  it("returns an unsubscribe that closes the connection", () => withFakeES(() => {
    const stop = subscribeLive("http://x/stream", { onSnapshot: vi.fn(), onError: vi.fn() });
    stop();
    expect(FakeEventSource.last!.closed).toBe(true);
  }));

  it("exposes the default stream url", () =>
    expect(DEFAULT_STREAM_URL).toBe("http://localhost:8080/v1/live/stream"));
});
