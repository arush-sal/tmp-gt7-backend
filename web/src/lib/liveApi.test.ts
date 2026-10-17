// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { setRecording, RecordingError } from "./liveApi";

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
