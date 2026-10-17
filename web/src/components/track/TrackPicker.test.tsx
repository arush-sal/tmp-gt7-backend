// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react";
import { TrackPicker } from "./TrackPicker";

test("TrackPicker lists items and reports a pick", () => {
  const onPick = vi.fn();
  render(<TrackPicker value={null} onPick={onPick}
    items={[{ slug: "suzuka", name: "Suzuka Circuit" }, { slug: "spa-francorchamps", name: "Spa" }]} />);
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "spa-francorchamps" } });
  expect(onPick).toHaveBeenCalledWith("spa-francorchamps");
});

test("TrackPicker shows a provisional chip only while the auto-detected track is provisional", () => {
  const items = [{ slug: "suzuka", name: "Suzuka Circuit" }];
  const { rerender } = render(<TrackPicker value="suzuka" onPick={() => {}} items={items} provisional />);
  expect(screen.getByText("provisional")).toBeInTheDocument();
  rerender(<TrackPicker value="suzuka" onPick={() => {}} items={items} />);
  expect(screen.queryByText("provisional")).toBeNull();
});
