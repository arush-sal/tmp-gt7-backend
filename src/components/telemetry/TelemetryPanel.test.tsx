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

  // NOTE (substitution): the brief's keyboard-sensor jsdom integration test proved
  // flaky — jsdom's getBoundingClientRect returns all-zero rects, so @dnd-kit's
  // sortableKeyboardCoordinates can't compute a distinct "next" drop target and the
  // Space/ArrowDown/Space sequence never fires a commit. Rather than leave a flaky
  // test in the suite, this stubs each card row's getBoundingClientRect (by DOM
  // order) so the keyboard sensor can measure real, distinct positions — a standard
  // workaround for testing @dnd-kit under jsdom. The pure `reorder()` function
  // (TelemetryPanel.reorder.test.ts) remains the source of truth for the reorder
  // logic itself; this test exists only to prove the keyboard-drag wiring end to end.
  it("reorders cards by keyboard drag and persists the new order", async () => {
    const KEY = "gt7:test-reorder";
    localStorage.clear();
    const store = createHistoryStore();
    const { container } = render(<TelemetryPanel store={store} storageKey={KEY} />);

    const rows = container.querySelectorAll<HTMLElement>(".telemetry-graph.accordion-item");
    rows.forEach((row, i) => {
      row.getBoundingClientRect = () =>
        ({ top: i * 100, bottom: i * 100 + 90, left: 0, right: 300, width: 300, height: 90, x: 0, y: i * 100, toJSON() {} });
    });

    // The first card's drag handle; @dnd-kit keyboard sensor: Space to lift, ArrowDown to move, Space to drop.
    const handles = screen.getAllByRole("button", { name: /drag/i });
    handles[0].focus();
    fireEvent.keyDown(handles[0], { key: " ", code: "Space" });
    // @dnd-kit's KeyboardSensor attaches its move listener via a setTimeout(0) after
    // activation; yield a macrotask so it's attached before the next key event.
    await new Promise(r => setTimeout(r, 0));
    fireEvent.keyDown(handles[0], { key: "ArrowDown", code: "ArrowDown" });
    fireEvent.keyDown(handles[0], { key: " ", code: "Space" });
    const saved = JSON.parse(localStorage.getItem(KEY)!);
    expect(saved.order[0]).toBe("brake"); // throttle moved down past brake
    expect(saved.order[1]).toBe("throttle");
  });
});
