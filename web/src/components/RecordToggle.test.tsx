// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import RecordToggle from "./RecordToggle";
import { RecordingError } from "../lib/liveApi";

afterEach(() => vi.restoreAllMocks());
const ok = () => vi.fn(async (r: boolean) => ({ recording: r, session_id: "s" }));

describe("RecordToggle", () => {
  it("confirms before turning OFF; cancel sends nothing", () => {
    const send = ok(); const c = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<RecordToggle sessionId="s" available recording send={send} />);
    fireEvent.click(screen.getByText("● REC"));
    expect(c).toHaveBeenCalledWith("Stop recording? This discards every lap stored for the current session.");
    expect(send).not.toHaveBeenCalled();
  });
  it("sends false after confirm", async () => {
    const send = ok(); vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<RecordToggle sessionId="s" available recording send={send} />);
    fireEvent.click(screen.getByText("● REC"));
    await waitFor(() => expect(send).toHaveBeenCalledWith(false, "s"));
  });
  it("turning ON sends true with no confirm", async () => {
    const send = ok(); const c = vi.spyOn(window, "confirm");
    render(<RecordToggle sessionId="s" available recording={false} send={send} />);
    fireEvent.click(screen.getByText("○ Not recording"));
    await waitFor(() => expect(send).toHaveBeenCalledWith(true, "s"));
    expect(c).not.toHaveBeenCalled();
  });
  it("hidden when unavailable", () => {
    render(<RecordToggle sessionId="s" available={false} recording send={ok()} />);
    expect(screen.queryByText(/REC|Not recording/)).toBeNull();
  });
  it("disabled in flight; 409 shows the inline error", async () => {
    let rej!: (e: unknown) => void;
    const send = vi.fn(() => new Promise<never>((_, r) => { rej = r; }));
    render(<RecordToggle sessionId="s" available recording={false} send={send as never} />);
    const b = screen.getByText("○ Not recording") as HTMLButtonElement;
    fireEvent.click(b);
    await waitFor(() => expect(b.disabled).toBe(true));
    rej(new RecordingError(409));
    expect(await screen.findByText("No session to record")).toBeTruthy();
    expect(b.disabled).toBe(false);
  });
  it("replay confirm text keeps the original", () => {
    const c = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<RecordToggle sessionId="s" duplicate available recording send={ok()} />);
    fireEvent.click(screen.getByText("● REC"));
    expect(c).toHaveBeenCalledWith("Stop recording this replay? The stored original session is kept.");
  });
  it.each([
    [409, "session changed", "Session changed, try again"],
    [503, "stalled", "Telemetry stalled, recording unchanged"],
  ])("%i maps to its chip", async (status, message, text) => {
    const send = vi.fn(async () => { throw new RecordingError(status, message); });
    render(<RecordToggle sessionId="s" available recording={false} send={send as never} />);
    fireEvent.click(screen.getByText("○ Not recording"));
    expect(await screen.findByText(text)).toBeTruthy();
  });
});
