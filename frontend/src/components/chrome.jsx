import { useEffect, useState } from "react";
import { BookOpenText, Library, Menu, PackageOpen } from "lucide-react";
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerTitle, DrawerTrigger } from "@/components/ui/drawer";
import { cn } from "@/lib/utils";

export const SECTIONS = [
  { id: "clone", label: "Pack a website", short: "Pack", icon: PackageOpen },
  { id: "history", label: "Your archives", short: "Shelf", icon: Library },
  { id: "how-it-works", label: "Field notes", short: "Notes", icon: BookOpenText },
];

function useActiveSection() {
  const [active, setActive] = useState("clone");
  useEffect(() => {
    const observer = new IntersectionObserver(entries => {
      const hit = entries.filter(e => e.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
      if (hit) setActive(hit.target.id);
    }, { rootMargin: "-40% 0px -50% 0px", threshold: [0, .25, .5, 1] });
    SECTIONS.forEach(({ id }) => { const el = document.getElementById(id); if (el) observer.observe(el); });
    return () => observer.disconnect();
  }, []);
  return active;
}

export function Wordmark() {
  return (
    <a href="#clone" className="group flex items-center gap-3">
      <span className="pane grid size-11 place-items-center rounded-[14px] transition-transform group-hover:-rotate-6"><img src="/mark.svg" alt="" width={40} height={40} className="size-5 rounded-[5px]" /></span>
      <span className="leading-none">
        <span className="font-display block text-xl font-semibold tracking-[-0.04em]">pagepack</span>
        <span className="etched font-mono mt-1 block text-[10px] tracking-[0.14em] uppercase">static archiver</span>
      </span>
    </a>
  );
}

/** Desktop: a vertical glass ornament docked to the left edge, the way spatial apps attach tab bars. */
export function Ornament() {
  const active = useActiveSection();
  return (
    <nav aria-label="Main navigation" className="pane fixed top-1/2 left-5 z-40 hidden -translate-y-1/2 flex-col gap-1.5 rounded-[26px] p-2 lg:flex">
      {SECTIONS.map(({ id, label, short, icon: Icon }) => (
        <a key={id} href={`#${id}`} aria-label={label} aria-current={active === id ? "true" : undefined} className={cn("group relative grid size-12 place-items-center rounded-[18px] transition-all", active === id ? "keycap" : "text-foreground/60 hover:text-foreground hover:bg-white/70")}>
          <Icon className="size-5" strokeWidth={1.75} />
          <span className="pane pointer-events-none absolute left-full ml-4 rounded-full px-3 py-1.5 text-xs font-medium whitespace-nowrap text-foreground opacity-0 transition-all group-hover:translate-x-0 group-hover:opacity-100 -translate-x-1">{short}</span>
        </a>
      ))}
    </nav>
  );
}

/** Mobile: a floating glass button that raises a bottom sheet. */
export function MobileMenu() {
  return (
    <Drawer>
      <DrawerTrigger asChild>
        <button type="button" aria-label="Open menu" className="pane fixed top-5 right-5 z-40 grid size-12 place-items-center rounded-full lg:hidden"><Menu className="size-5" /></button>
      </DrawerTrigger>
      <DrawerContent className="slab border-0 bg-transparent data-[vaul-drawer-direction=bottom]:rounded-t-[32px] data-[vaul-drawer-direction=bottom]:border-t-0">
        <DrawerTitle className="sr-only">Menu</DrawerTitle>
        <DrawerDescription className="sr-only">Navigate Pagepack</DrawerDescription>
        <div className="grid gap-2 p-5 pb-8">
          {SECTIONS.map(({ id, label, icon: Icon }, i) => (
            <DrawerClose key={id} asChild>
              <a href={`#${id}`} className="pane flex items-center gap-4 rounded-2xl px-4 py-4 text-lg font-medium">
                <span className="keycap grid size-10 place-items-center rounded-xl"><Icon className="size-5" strokeWidth={1.75} /></span>
                {label}
                <span aria-hidden="true" className="etched ml-auto font-mono text-xs">0{i + 1}</span>
              </a>
            </DrawerClose>
          ))}
        </div>
      </DrawerContent>
    </Drawer>
  );
}
