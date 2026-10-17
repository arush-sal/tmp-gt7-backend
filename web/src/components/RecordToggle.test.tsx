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
    render(<RecordToggle live sessionId="s" available recording send={send} />);
    fireEvent.click(screen.getByText("● REC"));
    expect(c).toHaveBeenCalledWith("Stop recording? This discards every lap stored for the current session.");
    expect(send).not.toHaveBeenCalled();
  });
  it("sends false after confirm", async () => {
    const send = ok(); vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<RecordToggle live sessionId="s" available recording send={send} />);
    fireEvent.click(screen.getByText("● REC"));
    await waitFor(() => expect(send).toHaveBeenCalledWith(false, "s"));
  });
  it("turning ON sends true with no confirm", async () => {
    const send = ok(); const c = vi.spyOn(window, "confirm");
    render(<RecordToggle live sessionId="s" available recording={false} send={send} />);
    fireEvent.click(screen.getByText("● LIVE"));
    await waitFor(() => expect(send).toHaveBeenCalledWith(true, "s"));
    expect(c).not.toHaveBeenCalled();
  });
  it("live + recording: red REC button, aria-pressed", () => {
    render(<RecordToggle live sessionId="s" available recording send={ok()} />);
    const b = screen.getByRole("button", { name: /REC/ });
    expect(b.className).toMatch(/chip--red/);
    expect(b.getAttribute("aria-pressed")).toBe("true");
  });
  it("live + not recording: green LIVE button with the record tooltip", () => {
    render(<RecordToggle live sessionId="s" available recording={false} send={ok()} />);
    const b = screen.getByRole("button", { name: /LIVE/ });
    expect(b.className).toMatch(/chip--green/);
    expect(b.getAttribute("aria-pressed")).toBe("false");
    expect(b.title).toBe("Live telemetry, not recording. Click to record");
  });
  it("not live: grey OFFLINE status, not a button, click sends nothing", () => {
    const send = ok();
    render(<RecordToggle live={false} sessionId="s" available={false} recording send={send} />);
    expect(screen.queryByRole("button")).toBeNull();
    const st = screen.getByRole("status");
    expect(st.textContent).toBe("● OFFLINE");
    expect(st.getAttribute("aria-label")).toBe("not live");
    fireEvent.click(st);
    expect(send).not.toHaveBeenCalled();
  });
  it("live but recording unavailable (stored session): LIVE status only, tooltip, sends nothing", () => {
    const send = ok();
    render(<RecordToggle live sessionId="s" available={false} recording send={send} />);
    expect(screen.queryByRole("button")).toBeNull();
    const st = screen.getByRole("status");
    expect(st.textContent).toBe("● LIVE");
    expect(st.getAttribute("aria-label")).toBe("live");
    expect(st.title).toBe("Recording controls apply to the live session");
    fireEvent.click(st);
    expect(send).not.toHaveBeenCalled();
  });
  it("available but telemetry stale: OFFLINE, not clickable", () => {
    render(<RecordToggle live={false} sessionId="s" available recording send={ok()} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("● OFFLINE");
  });
  it("disabled in flight; 409 shows the inline error", async () => {
    let rej!: (e: unknown) => void;
    const send = vi.fn(() => new Promise<never>((_, r) => { rej = r; }));
    render(<RecordToggle live sessionId="s" available recording={false} send={send as never} />);
    const b = screen.getByText("● LIVE") as HTMLButtonElement;
    fireEvent.click(b);
    await waitFor(() => expect(b.disabled).toBe(true));
    rej(new RecordingError(409));
    expect(await screen.findByText("No session to record")).toBeTruthy();
    expect(b.disabled).toBe(false);
  });
  it("replay confirm text keeps the original", () => {
    const c = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<RecordToggle live sessionId="s" duplicate available recording send={ok()} />);
    fireEvent.click(screen.getByText("● REC"));
    expect(c).toHaveBeenCalledWith("Stop recording this replay? The stored original session is kept.");
  });
  it.each([
    [409, "session changed", "Session changed, try again"],
    [503, "stalled", "Telemetry stalled, recording unchanged"],
  ])("%i maps to its chip", async (status, message, text) => {
    const send = vi.fn(async () => { throw new RecordingError(status, message); });
    render(<RecordToggle live sessionId="s" available recording={false} send={send as never} />);
    fireEvent.click(screen.getByText("● LIVE"));
    expect(await screen.findByText(text)).toBeTruthy();
  });
});
