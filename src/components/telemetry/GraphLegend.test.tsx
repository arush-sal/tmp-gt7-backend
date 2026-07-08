// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react";
import GraphLegend from "./GraphLegend";

describe("GraphLegend — single-series", () => {
  it("renders actual/median/reference and toggles a clicked series", () => {
    const onToggle = vi.fn();
    render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} referenceAvailable={false} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /median/i }));
    expect(onToggle).toHaveBeenCalledWith("median");
  });

  it("disables the reference chip until reference is available", () => {
    render(<GraphLegend variant="single" channels={["speed"]} hidden={[]} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: /reference/i })).toBeDisabled();
  });

  it("marks a hidden series as inactive (aria-pressed=false)", () => {
    render(<GraphLegend variant="single" channels={["speed"]} hidden={["median"]} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: /median/i })).toHaveAttribute("aria-pressed", "false");
  });
});

describe("GraphLegend — multi-series", () => {
  it("renders one chip per channel, no reference chip", () => {
    render(<GraphLegend variant="multi" channels={["tyreFL","tyreFR","tyreRL","tyreRR"]} hidden={[]} referenceAvailable={false} onToggle={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /reference/i })).toBeNull();
    expect(screen.getByRole("button", { name: /tyreFL/i })).toBeInTheDocument();
  });
});
