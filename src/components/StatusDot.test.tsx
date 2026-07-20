// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import StatusDot from "./StatusDot";

describe("StatusDot", () => {
  it("renders status-dot--on when live", () => {
    const { getByRole } = render(<StatusDot live={true} />);
    expect(getByRole("status").className).toContain("status-dot--on");
  });

  it("renders status-dot--off when not live", () => {
    const { getByRole } = render(<StatusDot live={false} />);
    expect(getByRole("status").className).toContain("status-dot--off");
  });
});
