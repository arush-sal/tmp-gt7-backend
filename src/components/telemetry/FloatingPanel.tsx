import { Rnd } from "react-rnd";
import { useRef, type ReactNode } from "react";
import { loadGeom, saveGeom, clampGeom, type PanelGeom } from "../../lib/panelGeometry";

interface Props {
  storageKey: string;
  defaultGeom: PanelGeom;
  dragHandleClassName: string; // drag only by this element (the card/panel header)
  minWidth?: number;
  minHeight?: number;
  children: ReactNode;
}

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
  return (
    <Rnd
      default={{ x: geom.current.x, y: geom.current.y, width: geom.current.width, height: geom.current.height }}
      bounds="parent"
      dragHandleClassName={dragHandleClassName}
      minWidth={minWidth}
      minHeight={minHeight}
      maxWidth="100%"   // hard resize cap in addition to bounds="parent"
      maxHeight="100%"
      className="floating-panel"
      onDragStop={(_e, d) => persist({ ...geom.current, x: d.x, y: d.y })}
      onResizeStop={(_e, _dir, ref, _delta, pos) =>
        persist({ x: pos.x, y: pos.y, width: ref.offsetWidth, height: ref.offsetHeight })}
    >
      {children}
    </Rnd>
  );
}
