// @vitest-environment jsdom
import { render, screen, within, fireEvent } from "@testing-library/react";
import TelemetryPanel from "./TelemetryPanel";
import { createHistoryStore } from "../../lib/historyStore";

// uPlot needs layout; jsdom gives 0x0, so the ResizeObserver never builds a chart — that's fine,
// we assert the card scaffolding (titles, legends), not canvas pixels.
describe("TelemetryPanel", () => {
  it("renders a card per default key with its title", () => {
    render(<TelemetryPanel store={createHistoryStore()} />);
    for (const t of ["Throttle", "Brake", "Speed", "Gear", "Tyre Temp", "Fuel"]) {
      expect(screen.getByText(new RegExp(t, "i"))).toBeInTheDocument();
    }
  });

  it("tyre card shows exactly the 4 corner chips, no median/reference/current", () => {
    render(<TelemetryPanel store={createHistoryStore()} />);
    const tyreCard = screen.getByText(/Tyre Temp/i).closest(".accordion-item") as HTMLElement;
    const q = within(tyreCard);
    for (const corner of ["tyreFL", "tyreFR", "tyreRL", "tyreRR"]) {
      expect(q.getByRole("button", { name: new RegExp(corner, "i") })).toBeInTheDocument();
    }
    expect(q.queryByRole("button", { name: /median/i })).toBeNull();
    expect(q.queryByRole("button", { name: /reference/i })).toBeNull();
    expect(q.queryByRole("button", { name: /current/i })).toBeNull();
  });

  it("gives single-series cards a median chip", () => {
    render(<TelemetryPanel store={createHistoryStore()} />);
    expect(screen.getAllByRole("button", { name: /median/i }).length).toBeGreaterThan(0);
  });

  it("persists a removed card across remount", () => {
    const KEY = "gt7:test-panel";
    localStorage.clear();
    const store = createHistoryStore();
    const { unmount } = render(<TelemetryPanel store={store} storageKey={KEY} />);
    fireEvent.click(screen.getByRole("button", { name: /remove Speed/i }));
    expect(screen.queryByText(/^Speed$/)).toBeNull();
    unmount();
    render(<TelemetryPanel store={store} storageKey={KEY} />);
    expect(screen.queryByText(/^Speed$/)).toBeNull();
  });

  it("adds a card from the picker and persists it across remount", () => {
    const KEY = "gt7:test-panel-add";
    localStorage.clear();
    const store = createHistoryStore();
    const { unmount } = render(<TelemetryPanel store={store} storageKey={KEY} />);
    fireEvent.click(screen.getByRole("button", { name: /add graph/i }));
    fireEvent.click(screen.getByRole("button", { name: /^RPM$/i }));
    expect(screen.getByText(/^RPM$/)).toBeInTheDocument();
    unmount();
    render(<TelemetryPanel store={store} storageKey={KEY} />);
    expect(screen.getByText(/^RPM$/)).toBeInTheDocument(); // survived reload
  });
});
