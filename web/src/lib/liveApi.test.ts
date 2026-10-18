// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { setRecording, RecordingError, setLiveTrack, clearLiveTrack, TrackError, trackErrorText } from "./liveApi";

afterEach(() => vi.unstubAllGlobals());
const stub = (status: number, body: unknown = {}) => {
  const spy = vi.fn(async (_u: string, _i?: RequestInit) => ({ ok: status < 300, status, json: async () => body }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
};

describe("setRecording", () => {
  it("POSTs {recording} and returns the 200 body", async () => {
    const spy = stub(200, { recording: false, session_id: "s1" });
    await expect(setRecording(false, "s1")).resolves.toEqual({ recording: false, session_id: "s1" });
    const [url, init] = spy.mock.calls[0];
    expect(url).toBe("/v1/live/recording");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe('{"recording":false,"session_id":"s1"}');
  });
  it("throws RecordingError carrying 409", async () => {
    stub(409);
    await expect(setRecording(true, "")).rejects.toMatchObject({ status: 409 });
    await expect(setRecording(true, "")).rejects.toBeInstanceOf(RecordingError);
  });
  it("carries the error JSON message", async () => {
    stub(409, { error: "session changed" });
    await expect(setRecording(true, "x")).rejects.toMatchObject({ status: 409, message: "session changed" });
  });
});

describe("setLiveTrack / clearLiveTrack", () => {
  it("POSTs {track_slug} as JSON and returns the 200 body", async () => {
    const spy = stub(200, { track_slug: "suzuka", applies_to: "current", session_id: "s1" });
    await expect(setLiveTrack("suzuka")).resolves.toMatchObject({ applies_to: "current" });
    const [url, init] = spy.mock.calls[0];
    expect(url).toBe("/v1/live/track");
    expect(init?.method).toBe("POST");
    expect((init?.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
    expect(init?.body).toBe('{"track_slug":"suzuka"}');
  });
  it("DELETEs to clear", async () => {
    const spy = stub(200, { track_slug: null });
    await clearLiveTrack();
    const [url, init] = spy.mock.calls[0];
    expect(url).toBe("/v1/live/track");
    expect(init?.method).toBe("DELETE");
  });
  it("throws TrackError carrying the status; text maps 404/415/403/other", async () => {
    stub(404, { error: "unknown track" });
    await expect(setLiveTrack("x")).rejects.toMatchObject({ status: 404 });
    expect(trackErrorText(new TrackError(404))).toBe("Unknown track");
    expect(trackErrorText(new TrackError(415))).toBe("Request rejected");
    expect(trackErrorText(new TrackError(403))).toBe("Request rejected");
    expect(trackErrorText(new TypeError("network"))).toBe("Couldn't confirm track");
  });
});
