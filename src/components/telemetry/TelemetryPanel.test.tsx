// @vitest-environment jsdom
import { render, screen, within } from "@testing-library/react";
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
});
