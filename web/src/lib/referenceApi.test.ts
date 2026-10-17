import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchStoredReference, pinReference, clearPin, fetchTrackLaps, RefError, refErrorText } from "./referenceApi";
import { storedRef } from "../mock/storedRef";

const stub = (status: number, body: unknown = {}) => {
  const spy = vi.fn(async (_u: string, _i?: RequestInit) => ({ ok: status < 400, status, json: async () => body }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
};
afterEach(() => vi.unstubAllGlobals());

describe("referenceApi", () => {
  it("GET 200 returns the DTO; URL carries slug + decimate", async () => {
    const spy = stub(200, storedRef());
    expect((await fetchStoredReference("suzuka", 500))?.source).toBe("pinned");
    expect(spy.mock.calls[0][0]).toBe("/v1/tracks/suzuka/reference-lap?decimate=500");
  });
  it("GET 404 -> null (none stored)", async () => {
    stub(404, { error: "no stored reference for track" });
    expect(await fetchStoredReference("suzuka")).toBeNull();
  });
  it("GET 500 throws RefError", async () => {
    stub(500);
    await expect(fetchStoredReference("suzuka")).rejects.toMatchObject({ status: 500 });
  });
  it("PUT 200 sends JSON content type + body", async () => {
    const spy = stub(200, storedRef());
    await pinReference("suzuka", "sess-A", 2);
    const init = spy.mock.calls[0][1]!;
    expect(init.method).toBe("PUT");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
    expect(JSON.parse(init.body as string)).toEqual({ session_id: "sess-A", lap_number: 2 });
  });
  it.each([400, 404, 409, 415, 403])("PUT %i throws RefError with the status", async status => {
    stub(status, { error: "x" });
    const err = await pinReference("suzuka", "s", 1).catch(e => e);
    expect(err).toBeInstanceOf(RefError);
    expect(err.status).toBe(status);
  });
  it("DELETE 200 returns the fallback, 404 -> null, no body/headers needed", async () => {
    const spy = stub(200, storedRef({ source: "fastest" }));
    expect((await clearPin("suzuka"))?.source).toBe("fastest");
    expect(spy.mock.calls[0][1]).toEqual({ method: "DELETE" });
    stub(404, {});
    expect(await clearPin("suzuka")).toBeNull();
  });
  it("lists track laps with limit", async () => {
    const spy = stub(200, [{ session_id: "s", lap_number: 1 }]);
    expect(await fetchTrackLaps("suzuka", 5)).toHaveLength(1);
    expect(spy.mock.calls[0][0]).toBe("/v1/tracks/suzuka/laps?limit=5");
  });
  it("maps statuses to chip text", () => {
    expect(refErrorText(new RefError(409))).toBe("Lap not usable as reference");
    expect(refErrorText(new RefError(404))).toBe("Lap not found");
    expect(refErrorText(new RefError(415))).toBe("Request rejected");
    expect(refErrorText(new RefError(403))).toBe("Request rejected");
    expect(refErrorText(new Error("net"))).toBe("Couldn't change reference");
  });
});
