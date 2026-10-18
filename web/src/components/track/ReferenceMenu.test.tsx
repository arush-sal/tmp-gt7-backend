// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import ReferenceMenu from "./ReferenceMenu";
import { storedRef } from "../../mock/storedRef";
import { RefError } from "../../lib/referenceApi";

const api = vi.hoisted(() => ({ fetchTrackLaps: vi.fn(), pinReference: vi.fn(), clearPin: vi.fn() }));
vi.mock("../../lib/referenceApi", async orig => ({ ...(await orig<typeof import("../../lib/referenceApi")>()), ...api }));

const lapRow = (n: number, pinned = false) => ({
  session_id: `s${n}`, lap_number: n, lap_time: "91.000s", recorded_at: "2026-07-02T10:00:00Z",
  car: { id: 7, name: "Other Car", category: null }, pinned,
});
const openMenu = () => fireEvent.click(screen.getByRole("button", { name: /Ref |No stored ref/ }));
beforeEach(() => { Object.values(api).forEach(f => f.mockReset()); });

describe("ReferenceMenu", () => {
  it("shows the chip with time and source, tooltip carries car details", () => {
    render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={() => {}} />);
    const chip = screen.getByText("Ref 1:30.685 · pinned");
    expect(chip.getAttribute("title")).toMatch(/Test GT3 \(id 123, Gr\.3\)/);
    expect(chip.getAttribute("title")).toMatch(/session lap 2/);
    expect(chip.getAttribute("title")).toMatch(/recorded/);
  });
  it("Change… lists laps; choosing one PUTs and notifies", async () => {
    api.fetchTrackLaps.mockResolvedValue([lapRow(3, true), lapRow(4)]);
    api.pinReference.mockResolvedValue(storedRef());
    const onChanged = vi.fn();
    render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={onChanged} />);
    openMenu();
    fireEvent.click(screen.getByText("Change…"));
    await screen.findByText(/★ pinned/);
    fireEvent.click(screen.getAllByText(/Other Car/)[1]);
    await waitFor(() => expect(api.pinReference).toHaveBeenCalledWith("suzuka", "s4", 4));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });
  it("Clear pin only when pinned; DELETEs", async () => {
    api.clearPin.mockResolvedValue(null);
    const onChanged = vi.fn();
    const { rerender } = render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={onChanged} />);
    openMenu();
    fireEvent.click(screen.getByText("Clear pin"));
    await waitFor(() => expect(api.clearPin).toHaveBeenCalledWith("suzuka"));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    rerender(<ReferenceMenu slug="suzuka" stored={storedRef({ source: "fastest" })} onChanged={onChanged} />);
    openMenu();
    expect(screen.queryByText("Clear pin")).toBeNull();
  });
  it("shows 'Request rejected' on 415", async () => {
    api.fetchTrackLaps.mockResolvedValue([lapRow(4)]);
    api.pinReference.mockRejectedValue(new RefError(415));
    render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={() => {}} />);
    openMenu();
    fireEvent.click(screen.getByText("Change…"));
    fireEvent.click(await screen.findByText(/Other Car/));
    expect((await screen.findByRole("alert")).textContent).toBe("Request rejected");
  });
  it("no stored ref: chip says so, Change… still works, empty list shows a message", async () => {
    api.fetchTrackLaps.mockResolvedValue([]);
    render(<ReferenceMenu slug="suzuka" stored={null} onChanged={() => {}} />);
    expect(screen.getByText("No stored ref")).toBeTruthy();
    expect(screen.queryByText("Clear pin")).toBeNull();
    openMenu();
    fireEvent.click(screen.getByText("Change…"));
    expect(await screen.findByText("No stored laps on this track yet")).toBeTruthy();
  });
  it("waiting (no confirmed slug): dim chip with the reason, no menu or pin actions", () => {
    render(<ReferenceMenu slug={null} waitingTip="The stored reference lap loads once the track is confirmed" stored={null} onChanged={() => {}} />);
    const chip = screen.getByText("Ref · confirming track…");
    expect(chip.getAttribute("title")).toBe("The stored reference lap loads once the track is confirmed");
    expect(screen.queryByText("Change…")).toBeNull();
    expect(screen.queryByText("Clear pin")).toBeNull();
  });
  it("chip carries the discoverability hint after the car details", () => {
    render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={() => {}} />);
    const t = screen.getByText("Ref 1:30.685 · pinned").getAttribute("title")!;
    expect(t.indexOf("Test GT3")).toBeLessThan(t.indexOf("Stored reference lap for this track. Click to change or clear the pin."));
  });
  it("panel is closed until the chip is clicked, portalled to body, and closes on outside click and Escape", () => {
    const { container } = render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={() => {}} />);
    expect(screen.queryByText("Change…")).toBeNull();
    openMenu();
    const pop = screen.getByText("Change…").closest(".ref-menu__pop")!;
    expect(pop.parentElement).toBe(document.body);
    expect(container.contains(pop)).toBe(false);
    expect(screen.getByRole("button", { name: /Ref / }).getAttribute("aria-expanded")).toBe("true");
    fireEvent.mouseDown(pop); // inside click keeps it open
    expect(screen.getByText("Change…")).toBeTruthy();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByText("Change…")).toBeNull();
    openMenu();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByText("Change…")).toBeNull();
  });
});
