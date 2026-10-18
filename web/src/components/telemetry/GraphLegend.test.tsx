// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react";
import GraphLegend from "./GraphLegend";

describe("GraphLegend — single-series", () => {
  it("renders median as a toggle button and calls onToggle('median') on click", () => {
    const onToggle = vi.fn();
    render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} medianAvailable={true} referenceAvailable={false} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /median/i }));
    expect(onToggle).toHaveBeenCalledWith("median");
  });

  it("renders current as a toggle button: pressed when shown, calls onToggle('actual')", () => {
    const onToggle = vi.fn();
    const { rerender } = render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} medianAvailable={true} referenceAvailable={false} onToggle={onToggle} />);
    const btn = screen.getByRole("button", { name: /current/i });
    expect(btn).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(btn);
    expect(onToggle).toHaveBeenCalledWith("actual");
    rerender(<GraphLegend variant="single" channels={["speed"]} hidden={["actual"]} medianAvailable={true} referenceAvailable={false} onToggle={onToggle} />);
    expect(screen.getByRole("button", { name: /current/i })).toHaveAttribute("aria-pressed", "false");
  });

  it("colours the median chip to match its line", () => {
    render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} medianAvailable={true} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: /median/i }).style.color).toBe("rgb(255, 140, 0)");
  });

  it("disables the reference chip until reference is available", () => {
    render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} medianAvailable={true} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: /reference/i })).toBeDisabled();
  });

  it("marks a hidden series as inactive (aria-pressed=false)", () => {
    render(<GraphLegend variant="single" channels={["speed"]} hidden={["median"]} medianAvailable={true} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: /median/i })).toHaveAttribute("aria-pressed", "false");
  });
});

describe("GraphLegend — multi-series", () => {
  it("renders one chip per channel, no reference chip", () => {
    render(<GraphLegend variant="multi" channels={["tyreFL","tyreFR","tyreRL","tyreRR"]} hidden={[]} medianAvailable={true} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /reference/i })).toBeNull();
    expect(screen.getByRole("button", { name: /tyreFL/i })).toBeInTheDocument();
  });
});
