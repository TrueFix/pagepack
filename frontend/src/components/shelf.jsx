import { useState } from "react";
import { ArrowDownToLine, ArrowUpRight, Check, CircleAlert, Loader2, LockKeyhole, RotateCcw, Trash2 } from "lucide-react";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { cn, hostOf, hueOf } from "@/lib/utils";
import { isFinished } from "@/useArchives";

const FILTERS = [{ id: "all", name: "All" }, { id: "active", name: "In progress" }, { id: "done", name: "Ready" }, { id: "error", name: "Stopped" }];
const STAGE = {
  pending: { name: "Queued", fill: 12, next: 18 },
  cloning: { name: "Downloading", fill: 18, next: 58 },
  zipping: { name: "Zipping", fill: 58, next: 72 },
  uploading: { name: "Uploading", fill: 72, next: 86 },
  done: { name: "Ready", fill: 86, next: 86 },
  error: { name: "Failed", fill: 22, next: 22 },
  canceled: { name: "Canceled", fill: 0, next: 0 },
};

/** Liquid level: each stage owns a band; known progress (zip/upload %, share of the size limit) fills it. */
function fillLevel(job) {
  const stage = STAGE[job.status] ?? STAGE.pending;
  const p = job.progress;
  const pct = p?.stage_percent ?? (job.status === "cloning" && p ? Math.min(100, Math.sqrt(p.limit_percent ?? 0) * 10) : 0);
  return stage.fill + (stage.next - stage.fill) * (pct / 100);
}

/** One line of live detail: what is happening right now, or what was produced. */
function details(job) {
  const p = job.progress, r = job.result;
  switch (job.status) {
    case "pending": return ["Waiting for a worker"];
    case "cloning": return p?.files ? [p.downloaded, `${p.files} ${p.files === 1 ? "file" : "files"}`, p.rate] : ["Connecting…"];
    case "zipping": return p ? [`${p.zipped_files ?? 0}/${p.files} files`, `${Math.round(p.stage_percent ?? 0)}%`] : ["Zipping…"];
    case "uploading": return p ? [`${p.uploaded ?? "0 B"} of ${p.archive_size}`, `${Math.round(p.stage_percent ?? 0)}%`] : ["Uploading…"];
    case "done": return r ? [`${r.archive_size} ZIP`, `${r.files} files`, r.space_saved_percent > 0 ? `${Math.round(r.space_saved_percent)}% smaller` : null, r.duration].filter(Boolean) : [];
    case "canceled": return ["Canceled · files cleaned up"];
    default: return [];
  }
}
function dateLabel(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? "Saved here" : date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }); }

/** A block of glass. Liquid rises as the job advances; bubbles while it's working; a crack if it failed. */
function Vessel({ host, status, fill }) {
  const hue = status === "error" ? 25 : hueOf(host.replace(/^www\./, ""));
  const active = !isFinished({ status });
  const letter = host.replace(/^www\./, "").charAt(0).toUpperCase() || "?";
  const top = `${100 - fill}%`;
  return (
    <div className="relative">
      <div aria-hidden="true" className="cast absolute inset-x-[8%] -bottom-6 h-10" style={{ "--cast-color": `oklch(0.72 0.16 ${hue} / ${status === "done" ? 70 : 45}%)` }} />
      <div className="vessel aspect-[4/5] rounded-[26px]" aria-hidden="true">
        <div className={cn("liquid z-0", active ? "animate-wave-slow" : "rotate-[24deg]")} style={{ top: `calc(${top} - 7px)`, background: `oklch(0.9 0.08 ${hue} / 55%)` }} />
        <div className={cn("liquid z-0", active ? "animate-wave" : "rotate-[8deg]")} style={{ top, background: `linear-gradient(180deg, oklch(0.88 0.1 ${hue} / 80%), oklch(0.76 0.14 ${(hue + 20) % 360} / 72%) 22%, oklch(0.66 0.16 ${(hue + 35) % 360} / 82%) 48%)`, boxShadow: "inset 0 2px 0 oklch(1 0 0 / 75%), inset 0 10px 24px -8px oklch(1 0 0 / 45%)" }} />
        <span className="font-display absolute inset-0 z-[1] grid place-items-center pb-[8%] text-[clamp(3.5rem,9vw,5.5rem)] leading-none font-bold tracking-[-0.06em]" style={{ color: `oklch(0.3 0.08 ${hue} / 80%)`, textShadow: "0 1px 0 oklch(1 0 0 / 60%)" }}>{letter}</span>
        {active && [18, 46, 72].map((x, i) => <i key={x} className="animate-bubble absolute bottom-3 z-[2] size-2 rounded-full bg-white/80 ring-1 ring-white" style={{ left: `${x}%`, animationDelay: `${i * -0.9}s` }} />)}
        {status === "error" && <svg className="absolute inset-0 z-[2] size-full" viewBox="0 0 100 125" preserveAspectRatio="none"><path d="M58 0 L50 22 L62 34 L44 58 L55 70 L40 98" fill="none" stroke="white" strokeWidth=".9" strokeLinejoin="round" opacity=".9" /><path d="M62 34 L78 40" fill="none" stroke="white" strokeWidth=".6" opacity=".7" /></svg>}
        {status === "done" && <span className="absolute top-3 right-3 z-[4] grid size-7 place-items-center rounded-full bg-white/85 text-success shadow-sm ring-1 ring-white"><Check className="size-4" strokeWidth={2.5} /></span>}
      </div>
    </div>
  );
}

function StatusLabel({ status }) {
  const tone = status === "done" ? "text-success" : status === "error" ? "text-destructive" : "text-foreground/70";
  return <span data-status={status} className={cn("archive-status inline-flex items-center gap-1.5 font-mono text-[11px] font-medium tracking-wide uppercase", tone)}>
    {status === "done" ? <Check className="size-3" /> : isFinished({ status }) ? <CircleAlert className="size-3" /> : <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />}{STAGE[status].name}
  </span>;
}

function Specimen({ job, onRetry, onDownload, onCancel, index }) {
  const [busy, setBusy] = useState(false);
  const [cancelMessage, setCancelMessage] = useState("");
  const host = hostOf(job.url);
  const line = details(job);
  const failures = job.result?.requests_failed || job.progress?.requests_failed;
  return (
    <li className="archive-row reveal flex flex-col" style={{ "--d": `${Math.min(index, 8) * 60}ms` }}>
      <Vessel host={host} status={job.status} fill={fillLevel(job)} />
      <div className="mt-7 min-w-0 px-1">
        <p className="font-display truncate text-lg leading-tight font-semibold tracking-[-0.03em]" title={job.url}>{host}</p>
        <p className="text-muted-foreground mt-0.5 font-mono text-[11px]">{dateLabel(job.created_at)}</p>
        {line.length > 0 && <p className="archive-detail text-foreground/75 mt-1.5 font-mono text-[11px] leading-relaxed" title={job.summary}>{line.join(" · ")}</p>}
        {job.status === "cloning" && job.progress?.last_url && <p className="text-muted-foreground mt-0.5 truncate font-mono text-[10px]" title={job.progress.last_url}>✓ {job.progress.last_url.replace(/^https?:\/\//, "")}</p>}
        {failures > 0 && !["error", "canceled"].includes(job.status) && <p className="text-signal mt-0.5 font-mono text-[10px]">{failures} failed {failures === 1 ? "request" : "requests"}</p>}
        {(job.error?.message || job.msg) && job.status === "error" && <p className="text-destructive mt-1.5 text-xs leading-snug">{job.error?.message || job.msg}</p>}
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 px-1">
        <span className="archive-stage"><StatusLabel status={job.status} /></span>
        {job.status === "done"
          ? <button type="button" className="archive-action keycap inline-flex h-9 items-center gap-1.5 rounded-xl px-3 text-sm font-medium" disabled={busy} aria-label={`Download archive of ${host}`} onClick={async () => { setBusy(true); try { await onDownload(job); } finally { setBusy(false); } }}>{busy ? <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" /> : <ArrowDownToLine className="size-3.5" />}ZIP</button>
          : isFinished(job)
            ? <button type="button" className="archive-action pane inline-flex h-9 items-center gap-1.5 rounded-xl px-3 text-sm font-medium" aria-label={`Pack ${host} again`} onClick={() => onRetry(job.url)}><RotateCcw className="size-3.5" />Retry</button>
            : <button type="button" className="archive-action pane inline-flex h-9 items-center rounded-xl px-3 text-sm font-medium disabled:opacity-50" disabled={busy} aria-label={`Cancel archive of ${host}`} onClick={async () => {
              setBusy(true); setCancelMessage("");
              try {
                const data = await onCancel(job);
                if (data.status === "canceling") setCancelMessage("Cancel requested. Waiting for cleanup…");
              } catch (error) { setCancelMessage(error.message); }
              finally { setBusy(false); }
            }}>{busy ? "Canceling…" : "Cancel"}</button>}
      </div>
      {cancelMessage && !isFinished(job) && <p role="status" className="text-muted-foreground mt-2 px-1 text-xs">{cancelMessage}</p>}
    </li>
  );
}

export default function Shelf({ archives, filter, setFilter, onRetry, onDownload, onCancel }) {
  const visible = archives.jobs.filter(job => filter === "all" || (filter === "active" ? !isFinished(job) : filter === "error" ? ["error", "canceled"].includes(job.status) : job.status === filter));
  const counts = { all: archives.jobs.length, active: archives.jobs.filter(j => !isFinished(j)).length, done: archives.jobs.filter(j => j.status === "done").length, error: archives.jobs.filter(j => ["error", "canceled"].includes(j.status)).length };
  return (
    <section id="history" aria-labelledby="history-heading" className="scroll-mt-8 px-4 py-24 sm:px-8 lg:px-28">
      <div className="mx-auto max-w-5xl">
        <div className="flex flex-wrap items-end justify-between gap-6">
          <div>
            <p className="etched font-mono text-[11px] tracking-[0.16em] uppercase">02 — The shelf</p>
            <h2 id="history-heading" className="font-display mt-3 flex items-start gap-3 text-[clamp(2.4rem,5vw,4rem)] leading-none font-semibold tracking-[-0.05em]">Your archives<span id="job-count" className="pane mt-1 rounded-full px-2.5 py-1 font-mono text-xs font-medium tracking-normal tabular-nums">{archives.jobs.length}</span></h2>
            <p className="text-muted-foreground mt-3 flex items-center gap-2 text-sm"><LockKeyhole className="size-3.5" />Kept only in this browser.</p>
          </div>
          <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            <Tabs value={filter} onValueChange={setFilter} aria-label="Filter archives" className="min-w-0 flex-1 sm:flex-none">
              <TabsList className="pane h-11! w-full rounded-2xl bg-transparent p-1 sm:w-fit">
                {FILTERS.map(item => (
                  <TabsTrigger key={item.id} value={item.id} data-filter={item.id} className="text-foreground/60 hover:text-foreground h-full rounded-xl border-0 px-2 text-xs data-[state=active]:shadow-none sm:px-3.5 sm:text-sm data-[state=active]:[background:linear-gradient(180deg,oklch(0.34_0.025_265),oklch(0.17_0.022_265))] data-[state=active]:text-white">
                    {item.name}{counts[item.id] > 0 && <span className="hidden font-mono text-[10px] opacity-60 sm:inline">{counts[item.id]}</span>}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <button type="button" id="clear-history" aria-label="Clear finished archives from this browser" title="Clear finished" disabled={!archives.jobs.some(isFinished)} onClick={archives.clear} className="pane text-foreground/70 hover:text-destructive grid size-11 shrink-0 place-items-center rounded-2xl transition-colors disabled:opacity-40"><Trash2 className="size-4" /></button>
          </div>
        </div>

        {archives.notice && <p className="text-destructive mt-6 text-sm" role="status">{archives.notice}</p>}

        {visible.length === 0
          ? <Empty id="empty-state" className="mt-14 border-0 p-0 md:p-0">
            <EmptyHeader className="max-w-xs">
              <EmptyMedia className="mb-8 w-32">
                <div className="relative w-full"><div className="cast absolute inset-x-[8%] -bottom-6 h-10" style={{ "--cast-color": "oklch(0.75 0.1 250 / 50%)" }} /><div className="vessel animate-bob aspect-[4/5] w-full rounded-[22px]" /></div>
              </EmptyMedia>
              <EmptyTitle className="font-display text-2xl font-semibold tracking-[-0.03em]">{filter === "all" ? "An empty shelf" : "Nothing in this view"}</EmptyTitle>
              <EmptyDescription>{filter === "all" ? "Pack your first website and it’ll appear here as it fills." : "Try another filter, or pack something new."}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent><button type="button" id="empty-action" className="keycap inline-flex h-11 items-center gap-1.5 rounded-2xl px-5 text-sm font-medium" onClick={() => onRetry()}>Create archive<ArrowUpRight className="size-4" /></button></EmptyContent>
          </Empty>
          : <ul aria-label="Archives" className="mt-14 grid grid-cols-2 gap-x-4 gap-y-12 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4">
            {visible.map((job, i) => <Specimen key={job.request_id} job={job} index={i} onRetry={onRetry} onDownload={onDownload} onCancel={onCancel} />)}
          </ul>}

        <p className="text-muted-foreground mt-16 max-w-md font-mono text-[11px] leading-relaxed">Clearing removes entries from this browser, not stored ZIPs. Archives are temporary — download the copies you want to keep.</p>
      </div>
    </section>
  );
}
