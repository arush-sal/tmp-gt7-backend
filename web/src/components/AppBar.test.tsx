// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { useState } from "react";
import { createPortal } from "react-dom";
import AppBar from "./AppBar";
import RecordToggle from "./RecordToggle";

function Harness() {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  return (
    <>
      <AppBar theme="dark" onToggleTheme={() => {}} live recordSlotRef={setSlot} />
      {slot && createPortal(<RecordToggle live available recording sessionId="s" />, slot)}
    </>
  );
}

describe("AppBar status chip", () => {
  it("hosts the merged chip in the bar and shows exactly one indicator (no separate dot)", () => {
    const { container } = render(<Harness />);
    const bar = container.querySelector(".appbar")!;
    expect(bar.contains(screen.getByText("● REC"))).toBe(true);
    expect(bar.querySelector(".status-dot")).toBeNull();
    expect(bar.querySelectorAll("[role=status], .appbar__record button")).toHaveLength(1);
  });
  it("without a hosted dashboard (onboarding) falls back to the plain status dot", () => {
    const { container } = render(<AppBar theme="light" onToggleTheme={() => {}} live={false} />);
    expect(container.querySelector(".appbar__record")).toBeNull();
    expect(screen.getByRole("status").getAttribute("aria-label")).toBe("not live");
  });
});
