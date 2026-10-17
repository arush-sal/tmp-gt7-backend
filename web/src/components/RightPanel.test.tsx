// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import RightPanel from "./RightPanel";

const base = { view: null, corners: [], selectedCornerId: null, onCornerSelect: () => {}, selectedCornerDetail: null };

describe("RightPanel lap history reference actions", () => {
  it("shows Set-as-reference only on eligible laps, a star on the reference lap, and fires the callback", () => {
    const onSet = vi.fn();
    render(<RightPanel {...base} onSetReference={onSet} lapHistory={[
      { lap: 3, time: "1:31.000", best: false, canSetRef: true },
      { lap: 2, time: "1:30.685", best: true, canSetRef: true, isRef: true },
      { lap: 1, time: "1:40.000", best: false, canSetRef: false },
    ]} />);
    expect(screen.getAllByRole("button", { name: /as reference/i })).toHaveLength(1);
    expect(screen.getByLabelText("Stored reference")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Set lap 3 as reference" }));
    expect(onSet).toHaveBeenCalledWith(3);
  });
  it("renders the inline error chip", () => {
    render(<RightPanel {...base} refError="Lap not usable as reference" lapHistory={[]} />);
    expect(screen.getByRole("alert").textContent).toBe("Lap not usable as reference");
  });
});
