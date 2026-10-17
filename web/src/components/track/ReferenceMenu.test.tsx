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
    fireEvent.click(screen.getByText("Clear pin"));
    await waitFor(() => expect(api.clearPin).toHaveBeenCalledWith("suzuka"));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    rerender(<ReferenceMenu slug="suzuka" stored={storedRef({ source: "fastest" })} onChanged={onChanged} />);
    expect(screen.queryByText("Clear pin")).toBeNull();
  });
  it("shows 'Request rejected' on 415", async () => {
    api.fetchTrackLaps.mockResolvedValue([lapRow(4)]);
    api.pinReference.mockRejectedValue(new RefError(415));
    render(<ReferenceMenu slug="suzuka" stored={storedRef()} onChanged={() => {}} />);
    fireEvent.click(screen.getByText("Change…"));
    fireEvent.click(await screen.findByText(/Other Car/));
    expect((await screen.findByRole("alert")).textContent).toBe("Request rejected");
  });
});
