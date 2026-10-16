import { CornerDownLeft, Loader2, PackageCheck, Plus } from "lucide-react";
import { cn, hueOf } from "@/lib/utils";

const SAMPLES = [
  { label: "Try a simple page", url: "https://example.com" },
  { label: "The first website", url: "https://info.cern.ch" },
  { label: "A classic static site", url: "https://neverssl.com" },
  { label: "Explore GNU", url: "https://www.gnu.org" },
];

function typedHost(value) {
  try { const url = new URL(value.trim()); return /^https?:$/.test(url.protocol) && url.hostname ? url.hostname.replace(/^www\./, "") : null; } catch { return null; }
}

/**
 * The workbench. Whatever domain you type is set in enormous ink type that sinks
 * behind a slab of solid glass — half sharp in the air, half blurred inside the glass.
 */
export default function Workbench({ value, setValue, onSubmit, busy, config, configError, message, inputRef, onReset }) {
  const host = typedHost(value);
  const word = host ?? "keep.";
  const hue = host ? hueOf(host) : 265;
  const limit = config ? `${(config.max_website_bytes / 1024 ** 2).toLocaleString(undefined, { maximumFractionDigits: 1 })} MB max per website` : configError ? "Server unavailable" : "Connecting to Pagepack…";
  const state = config ? { label: "Ready", dot: "bg-success" } : configError ? { label: "Offline", dot: "bg-destructive" } : { label: "Connecting", dot: "bg-signal animate-pulse" };
  function pick(event, url) { event.preventDefault(); setValue(url); inputRef.current?.focus(); }

  return (
    <section id="clone" aria-labelledby="clone-heading" className="relative flex min-h-svh scroll-mt-0 flex-col justify-center overflow-x-clip px-4 pt-28 pb-16 sm:px-8 lg:px-28">
      <div className="mx-auto w-full max-w-5xl">
        <div className="reveal flex flex-wrap items-end justify-between gap-x-10 gap-y-4">
          <h1 id="clone-heading" className="font-display text-[clamp(2.4rem,5.2vw,4.4rem)] leading-[0.92] font-semibold tracking-[-0.05em]">Put a website<br />under glass.</h1>
          <p className="text-muted-foreground max-w-xs text-[15px] leading-relaxed text-pretty">Paste a link. Pagepack lifts its pages, styles and images into a single ZIP that’s yours to keep.</p>
        </div>

        <div className="relative mt-[clamp(4.5rem,10vw,8.5rem)]">
          <div aria-hidden="true" data-depth={16} className="pointer-events-none absolute top-0 left-1/2 z-0 select-none will-change-transform">
            <p key={word} className="font-display animate-in fade-in-0 slide-in-from-bottom-4 -translate-x-1/2 -translate-y-[56%] leading-[0.8] font-bold tracking-[-0.065em] whitespace-nowrap duration-700" style={{ fontSize: `min(25vw, ${(160 / Math.max(word.length, 5)).toFixed(1)}vw, 15rem)`, color: host ? `oklch(0.26 0.05 ${hue})` : "oklch(0.21 0.022 265)" }}>{word}</p>
          </div>

          <div className={cn("reveal slab streak relative z-10 rounded-[36px] p-3 sm:p-6", busy && "busy-sweep")} style={{ "--cast": `oklch(0.72 0.15 ${hue} / 45%)`, "--d": "120ms" }}>
            <div className="flex items-center justify-between gap-3 px-2 pt-1 sm:px-1 sm:pt-0">
              <span className="etched font-mono text-[11px] tracking-[0.16em] uppercase">01 — Insert a website</span>
              <span className="text-muted-foreground flex items-center gap-2 font-mono text-[11px]"><i className={cn("size-1.5 rounded-full", state.dot)} />{state.label}</span>
            </div>
            <form id="clone-form" onSubmit={onSubmit} className="mt-3 sm:mt-4">
              <div className="well focus-within:ring-ring/45 flex items-center gap-2 rounded-[24px] p-1.5 pl-2 transition-shadow focus-within:ring-2 sm:p-2 sm:pl-3">
                <input id="website" ref={inputRef} name="url" type="text" inputMode="url" autoComplete="url" autoCapitalize="none" spellCheck={false} required maxLength={2048} placeholder="https://a-site-worth-keeping.com" aria-label="Website URL" aria-describedby="limit" value={value} onChange={event => setValue(event.target.value)} className="font-display placeholder:text-foreground/28 h-13 min-w-0 flex-1 bg-transparent px-2 text-lg tracking-[-0.02em] outline-none sm:h-16 sm:text-2xl" />
                <button type="button" aria-label="New archive" title="Clear and start over" onClick={onReset} className="pane text-foreground/70 hover:text-foreground grid size-11 shrink-0 place-items-center rounded-full transition-transform hover:rotate-90"><Plus className="size-5" /></button>
              </div>
              <div className="mt-4 flex flex-wrap items-center justify-between gap-3 px-2 pb-1 sm:px-1 sm:pb-0">
                <p id="limit" className="text-muted-foreground font-mono text-[11px]">{limit} · No account needed</p>
                <button id="submit" type="submit" aria-label="Pack website" title="Pack website (Enter)" disabled={busy || !config} aria-busy={busy} className="keycap inline-flex h-12 w-full items-center justify-center gap-2.5 rounded-2xl pr-3 pl-5 font-medium sm:w-auto">
                  {busy ? <Loader2 className="size-4 animate-spin motion-reduce:animate-none" /> : <PackageCheck className="size-4" />}
                  {busy ? "Packing" : "Pack it"}
                  <kbd aria-hidden="true" className="grid size-6 place-items-center rounded-md bg-white/12 ring-1 ring-white/20"><CornerDownLeft className="size-3" /></kbd>
                </button>
              </div>
            </form>
          </div>
        </div>

        <div className="mt-8 flex min-h-6 justify-center">
          {(message || configError) && <p id="form-message" role="status" className={cn("pane rounded-full px-4 py-2 text-sm", message?.error || configError ? "text-destructive" : "text-success")}>{message?.text || configError}</p>}
        </div>

        <div className="reveal mt-4 flex flex-wrap items-center justify-center gap-2" style={{ "--d": "240ms" }}>
          <span className="etched mr-1 font-mono text-[11px] tracking-[0.16em] uppercase">or try</span>
          {SAMPLES.map(({ label, url }) => (
            <a key={url} href={url} onClick={event => pick(event, url)} className="pane group inline-flex items-center gap-2 rounded-full py-1.5 pr-3.5 pl-1.5 text-sm transition-transform hover:-translate-y-0.5">
              <span aria-hidden="true" className="size-5 rounded-full" style={{ background: `radial-gradient(circle at 35% 30%, white, oklch(0.78 0.13 ${hueOf(new URL(url).hostname.replace(/^www\./, ""))}) 70%)` }} />
              {label}
            </a>
          ))}
        </div>
      </div>
    </section>
  );
}
