import { useEffect } from "react";

/**
 * Pointer parallax for elements marked `data-depth`.
 * Writes a transform straight onto those few nodes (no inherited CSS variables, so no
 * page-wide style recalculation), eases with one rAF loop, and stops once settled.
 */
export function useParallax() {
  useEffect(() => {
    if (!window.matchMedia("(pointer: fine) and (prefers-reduced-motion: no-preference)").matches) return;
    let tx = 0, ty = 0, x = 0, y = 0, frame = 0, nodes = [];
    function tick() {
      x += (tx - x) * 0.09;
      y += (ty - y) * 0.09;
      for (const [el, depth] of nodes) el.style.transform = `translate3d(${(x * depth).toFixed(1)}px, ${(y * depth).toFixed(1)}px, 0)`;
      frame = Math.abs(tx - x) + Math.abs(ty - y) > 0.002 ? requestAnimationFrame(tick) : 0;
    }
    function move(e) {
      tx = e.clientX / innerWidth * 2 - 1;
      ty = e.clientY / innerHeight * 2 - 1;
      if (!frame) {
        nodes = [...document.querySelectorAll("[data-depth]")].filter(el => el.offsetParent !== null).map(el => [el, Number(el.dataset.depth)]);
        frame = requestAnimationFrame(tick);
      }
    }
    window.addEventListener("pointermove", move, { passive: true });
    return () => { window.removeEventListener("pointermove", move); cancelAnimationFrame(frame); };
  }, []);
}

/** A solid glass marble: specular point, coloured core, light caught on the far edge. Static unless the pointer moves. */
export function Marble({ size, hue, depth = 10, className }) {
  return (
    <div aria-hidden="true" data-depth={depth} className={`pointer-events-none absolute will-change-transform ${className}`}>
      <div className="rounded-full" style={{
        width: size, height: size,
        background: `radial-gradient(circle at 31% 26%, oklch(1 0 0) 0 4%, oklch(1 0 0 / 70%) 9%, transparent 26%),
          radial-gradient(circle at 68% 80%, oklch(0.88 0.13 ${hue} / 85%), transparent 52%),
          radial-gradient(circle at 50% 45%, oklch(0.97 0.02 ${hue} / 40%), oklch(0.8 0.1 ${hue} / 45%) 75%, oklch(0.7 0.13 ${hue} / 55%))`,
        boxShadow: `inset -${size / 9}px -${size / 7}px ${size / 4}px oklch(0.6 0.14 ${hue} / 35%), inset ${size / 14}px ${size / 12}px ${size / 6}px oklch(1 0 0 / 90%), 0 ${size / 3}px ${size / 2}px -${size / 4}px oklch(0.55 0.14 ${hue} / 45%)`,
      }} />
    </div>
  );
}

/** The room. Light pools, base gradient and grain are one static layer, painted once. */
export function Studio() {
  return (
    <div aria-hidden="true" className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <div className="studio-light absolute inset-0" />
      <Marble size={120} hue={40} depth={-30} className="top-[16vh] right-[9vw] hidden sm:block" />
      <Marble size={70} hue={235} depth={-45} className="top-[58vh] left-[7vw] hidden md:block" />
      <Marble size={44} hue={150} depth={-60} className="top-[78vh] right-[18vw] hidden sm:block" />
      <Marble size={26} hue={300} depth={-75} className="top-[30vh] left-[22vw] hidden lg:block" />
    </div>
  );
}
