import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

// A hover tooltip that renders into document.body (position: fixed) so it escapes any
// `overflow: hidden` ancestor (the header bar and cards all clip). Shows after a 0.5s dwell.
// The anchor gets a dotted underline + help cursor so users know a hint exists.
export default function Tooltip({ text, children }: { text: string; children: ReactNode }) {
  const [pos, setPos] = useState<{ x: number; y: number; below: boolean } | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const show = (e: React.MouseEvent) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const below = r.top < 80; // near the viewport top (e.g. header labels) -> drop the bubble below
    const x = r.left + r.width / 2;
    const y = below ? r.bottom : r.top;
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setPos({ x, y, below }), 500);
  };
  const hide = () => { window.clearTimeout(timer.current); setPos(null); };

  return (
    <span className="tip-anchor" onMouseEnter={show} onMouseLeave={hide}>
      {children}
      {pos && createPortal(
        <div
          className="tip-bubble"
          style={{
            left: pos.x, top: pos.y,
            transform: pos.below ? "translate(-50%, 8px)" : "translate(-50%, calc(-100% - 8px))",
          }}
          role="tooltip"
        >{text}</div>,
        document.body,
      )}
    </span>
  );
}
