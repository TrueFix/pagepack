import { useEffect, useRef, useState } from "react";
import { Studio, useParallax } from "@/components/studio";
import { MobileMenu, Ornament, Wordmark } from "@/components/chrome";
import Workbench from "@/components/workbench";
import Shelf from "@/components/shelf";
import Notes from "@/components/notes";
import { jobPath, request, useArchives } from "./useArchives";

export default function App() {
  useParallax();
  const archives = useArchives();
  const [config, setConfig] = useState(null);
  const [configError, setConfigError] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(null);
  const [filter, setFilter] = useState("all");
  const inputRef = useRef(null);

  useEffect(() => {
    const controller = new AbortController();
    request("/api/v1/config", undefined, controller.signal).then(setConfig).catch(() => { if (!controller.signal.aborted) setConfigError("Couldn’t connect to Pagepack. Refresh to try again."); });
    return () => controller.abort();
  }, []);

  async function submit(event) {
    event.preventDefault();
    if (busy || !config) return;
    let url;
    try {
      url = new URL(value.trim());
      if (!["https:", "http:"].includes(url.protocol) || !url.hostname || url.username || url.password || /\s/.test(value.trim())) throw new Error();
    } catch { setMessage({ text: "Enter one valid HTTP or HTTPS website URL without credentials.", error: true }); return; }
    setBusy(true); setMessage(null);
    try {
      const job = await request("/api/v1/jobs", { url: url.href });
      archives.add(job); setFilter("all"); setValue("");
      setMessage({ text: "Archive started — it’s filling up on your shelf below.", error: false });
    } catch (error) { setMessage({ text: error.message, error: true }); }
    finally { setBusy(false); }
  }
  function retry(url = "") { setValue(url); inputRef.current?.focus({ preventScroll: true }); document.getElementById("clone").scrollIntoView({ block: "start" }); }
  function reset() { setValue(""); setMessage(null); inputRef.current?.focus(); }
  async function cancelJob(job) {
    const data = await request(`${jobPath(job.request_id)}/cancel`, {});
    if (data.status !== "canceling") archives.update(data);
    return data;
  }
  async function download(job) {
    try {
      const data = await request(jobPath(job.request_id));
      const link = data.download?.url;
      if (data.status !== "done" || !link) throw new Error("This archive isn’t available yet.");
      const url = new URL(link);
      if (!["https:", "http:"].includes(url.protocol)) throw new Error("Invalid download link");
      window.location.assign(url.href);
    } catch (error) {
      if (error.status === 404) archives.expire(job.request_id);
      setMessage({ text: error.message, error: true });
    }
  }

  return <>
    <Studio />
    <a className="pane sr-only z-50 rounded-full px-4 py-2 focus:not-sr-only focus:fixed focus:top-3 focus:left-3" href="#website">Skip to website input</a>
    <header className="absolute inset-x-0 top-0 z-30 px-5 py-5 sm:px-8 lg:px-28">
      <div className="mx-auto max-w-5xl"><Wordmark /></div>
    </header>
    <Ornament />
    <MobileMenu />
    <main>
      <Workbench value={value} setValue={setValue} onSubmit={submit} busy={busy} config={config} configError={configError} message={message} inputRef={inputRef} onReset={reset} />
      <Shelf archives={archives} filter={filter} setFilter={setFilter} onRetry={retry} onDownload={download} onCancel={cancelJob} />
      <Notes />
    </main>
    <footer className="relative overflow-x-clip px-4 pt-10 pb-8 sm:px-8 lg:px-28">
      <p aria-hidden="true" className="font-display pointer-events-none text-center text-[21vw] leading-[0.8] font-bold tracking-[-0.07em] text-transparent select-none" style={{ backgroundImage: "linear-gradient(180deg, oklch(0.2 0.022 265 / 16%), oklch(0.2 0.022 265 / 0%) 88%)", WebkitBackgroundClip: "text", backgroundClip: "text" }}>pagepack</p>
      <p className="text-muted-foreground relative mt-6 text-center font-mono text-[11px]">Made for the static web · <a href="#history" className="hover:text-foreground underline underline-offset-4">History stays in this browser</a></p>
    </footer>
  </>;
}
