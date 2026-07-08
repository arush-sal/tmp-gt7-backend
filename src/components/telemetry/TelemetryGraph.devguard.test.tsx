// @vitest-environment jsdom
import { render } from "@testing-library/react";
import TelemetryGraph from "./TelemetryGraph";
import { createHistoryStore } from "../../lib/historyStore";

// Series are built once at mount from `channels`/`median`; changing either afterwards without
// remounting (i.e. without changing the component's `key`) would desync series metadata from
// the per-frame data columns uPlot re-reads every tick. The dev guard warns when that happens.
describe("TelemetryGraph — dev guard", () => {
  it("does not warn when channels/median stay the same across rerenders", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const store = createHistoryStore();
    const { rerender } = render(
      <TelemetryGraph store={store} channels={["speed"]} label="Speed" median={true} />,
    );
    rerender(<TelemetryGraph store={store} channels={["speed"]} label="Speed" median={true} />);
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it("warns in dev when channels change identity after mount without a remount", () => {
    vi.stubEnv("DEV", true);
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const store = createHistoryStore();
      const { rerender } = render(
        <TelemetryGraph store={store} channels={["speed"]} label="Speed" median={true} />,
      );
      rerender(<TelemetryGraph store={store} channels={["throttle"]} label="Throttle" median={true} />);
      expect(warn).toHaveBeenCalledWith(expect.stringContaining("channels/median changed after mount"));
    } finally {
      warn.mockRestore();
      vi.unstubAllEnvs();
    }
  });

  it("does not warn when channels change if DEV is false", () => {
    vi.stubEnv("DEV", false);
    const spy = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const store = createHistoryStore();
      const { rerender } = render(<TelemetryGraph store={store} channels={["throttle"]} label="A" />);
      rerender(<TelemetryGraph store={store} channels={["brake"]} label="A" />);
      expect(spy).not.toHaveBeenCalled();
    } finally {
      spy.mockRestore();
      vi.unstubAllEnvs();
    }
  });
});
