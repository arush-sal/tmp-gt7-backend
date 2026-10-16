import { reorder } from "./TelemetryPanel";

describe("reorder", () => {
  it("moves an item to the target index", () => {
    expect(reorder(["a", "b", "c"], "a", "c")).toEqual(["b", "c", "a"]);
  });
  it("is a no-op for unknown or equal keys", () => {
    expect(reorder(["a", "b"], "a", "a")).toEqual(["a", "b"]);
    expect(reorder(["a", "b"], "x", "b")).toEqual(["a", "b"]);
  });
});
