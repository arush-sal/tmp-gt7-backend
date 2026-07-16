import { Rnd } from "react-rnd";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { loadGeom, saveGeom, clampGeom, type PanelGeom } from "../../lib/panelGeometry";

interface Props {
  storageKey: string;
  defaultGeom: PanelGeom;
  dragHandleClassName: string; // drag only by this element (the card/panel header)
  minWidth?: number;
  minHeight?: number;
  children: ReactNode;
}

// Height the pane shrinks to when collapsed -- just enough to show the child's own
// header bar (telemetry-panel__header / corner-detail__header are both ~this tall).
const COLLAPSED_HEIGHT = 42;

// A draggable + resizable floating container, confined to its positioned parent (the canvas),
// with geometry persisted to localStorage so a refresh keeps where the user put it.
export default function FloatingPanel({
  storageKey, defaultGeom, dragHandleClassName, minWidth = 240, minHeight = 180, children,
}: Props) {
  // Live geometry in a ref (NOT recomputed from render-time closure) so onDragStop always saves the
  // CURRENT width/height (set by the last resize) instead of a stale pre-resize size, and vice-versa.
  const geom = useRef<PanelGeom>(
    clampGeom(loadGeom(storageKey, defaultGeom), window.innerWidth, window.innerHeight, minWidth, minHeight)
  );
  const persist = (g: PanelGeom) => { geom.current = g; saveGeom(storageKey, g); };
  const rndRef = useRef<Rnd | null>(null);
  const [collapsed, setCollapsed] = useState(false);

  // Collapsing/expanding is a purely visual resize -- it does NOT touch `geom`/localStorage,
  // so the pre-collapse height is still sitting in geom.current.height when we expand again.
  useEffect(() => {
    const rnd = rndRef.current;
    if (!rnd) return;
    rnd.updateSize({
      width: geom.current.width,
      height: collapsed ? COLLAPSED_HEIGHT : geom.current.height,
    });
  }, [collapsed]);

  return (
    <Rnd
      ref={rndRef}
      default={{ x: geom.current.x, y: geom.current.y, width: geom.current.width, height: geom.current.height }}
      bounds="parent"
      dragHandleClassName={dragHandleClassName}
      minWidth={minWidth}
      minHeight={collapsed ? COLLAPSED_HEIGHT : minHeight}
      maxWidth="100%"   // hard resize cap in addition to bounds="parent"
      maxHeight="100%"
      enableResizing={!collapsed}
      className="floating-panel"
      onDragStop={(_e, d) => persist({ ...geom.current, x: d.x, y: d.y })}
      onResizeStop={(_e, _dir, ref, _delta, pos) =>
        persist({ x: pos.x, y: pos.y, width: ref.offsetWidth, height: ref.offsetHeight })}
    >
      <button
        type="button"
        className="floating-panel__collapse chip"
        aria-label={collapsed ? "expand panel" : "collapse panel"}
        aria-pressed={collapsed}
        title={collapsed ? "expand" : "collapse"}
        onMouseDown={e => e.stopPropagation()}
        onClick={() => setCollapsed(c => !c)}
      >
        {collapsed ? "↓" : "↑"}
      </button>
      <div className={collapsed ? "floating-panel__body floating-panel__body--collapsed" : "floating-panel__body"}>
        {children}
      </div>
    </Rnd>
  );
}
