// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react";
import GraphPicker from "./GraphPicker";

describe("GraphPicker", () => {
  it("lists addable catalog entries not already shown, and calls onAdd", () => {
    const onAdd = vi.fn();
    render(<GraphPicker shownKeys={["throttle"]} onAdd={onAdd} onClose={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /^Throttle$/i })).toBeNull(); // already shown
    fireEvent.click(screen.getByRole("button", { name: /^RPM$/i }));
    expect(onAdd).toHaveBeenCalledWith("rpm");
  });

  it("shows steering disabled (needs backend), non-addable", () => {
    const onAdd = vi.fn();
    render(<GraphPicker shownKeys={[]} onAdd={onAdd} onClose={vi.fn()} />);
    const steering = screen.getByRole("button", { name: /steering/i });
    expect(steering).toBeDisabled();
    fireEvent.click(steering);
    expect(onAdd).not.toHaveBeenCalled();
  });
});
