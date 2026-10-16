import { useEffect, useRef, useState } from "react";

const KEY = "pagepack.jobs.v1";
export const isFinished = job => ["done", "error", "canceled"].includes(job.status);
const statuses = ["pending", "cloning", "zipping", "uploading", "done", "error", "canceled"];
// Fields kept in localStorage. Download links are never stored: they expire and are re-signed on demand.
const KEPT = ["request_id", "url", "status", "msg", "created_at", "finished_at", "summary", "progress", "result", "error", "warnings"];
export const keep = job => Object.fromEntries(KEPT.filter(key => job[key] !== undefined).map(key => [key, job[key]]));
export const jobPath = id => `/api/v1/jobs/${encodeURIComponent(id)}`;
const trimHistory = jobs => { let finished = 0; return jobs.filter(job => !isFinished(job) || ++finished <= 100); };

export async function request(path, payload, signal) {
  const timeout = AbortSignal.timeout(20000);
  const response = await fetch(path, {
    method: payload ? "POST" : "GET",
    headers: payload ? { "Content-Type": "application/json" } : {},
    body: payload ? JSON.stringify(payload) : undefined,
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  });
  let data;
  try { data = await response.json(); } catch { throw new Error("The server couldn’t respond. Please try again."); }
  if (!response.ok) {
    const error = new Error(data.msg || data.error || "The request failed. Please try again.");
    error.status = response.status;
    throw error;
  }
  return data;
}

function loadHistory() {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) || "[]");
    const jobs = Array.isArray(saved) ? trimHistory(saved.filter(j => j && typeof j.url === "string" && typeof j.request_id === "string" && statuses.includes(j.status))) : [];
    return { jobs: jobs.map(keep), error: "" };
  } catch { return { jobs: [], error: "Browser history couldn’t be loaded. New archives will still work." }; }
}

export function useArchives() {
  const [initial] = useState(loadHistory);
  const [jobs, setJobs] = useState(initial.jobs);
  const [storageError, setStorageError] = useState(initial.error);
  const [pollingError, setPollingError] = useState("");
  const current = useRef(jobs);
  current.current = jobs;
  const hasActive = jobs.some(job => !isFinished(job));

  useEffect(() => {
    try {
      localStorage.setItem(KEY, JSON.stringify(jobs.map(keep)));
      setStorageError("");
    } catch { setStorageError("Browser storage is unavailable. Keep this tab open to follow your archives."); }
  }, [jobs]);

  useEffect(() => {
    if (!hasActive) { setPollingError(""); return; }
    const controller = new AbortController();
    let timer;
    async function poll() {
      if (!document.hidden) {
        const active = current.current.filter(job => !isFinished(job));
        const changes = new Map();
        let failed = false;
        for (let i = 0; i < active.length && !controller.signal.aborted; i += 4) {
          await Promise.all(active.slice(i, i + 4).map(async job => {
            try {
              const data = await request(jobPath(job.request_id), undefined, controller.signal);
              if (!statuses.includes(data.status)) throw new Error("Invalid job status");
              if (data.updated_at !== job.updated_at || data.status !== job.status) changes.set(job.request_id, { ...keep(data), updated_at: data.updated_at });
            } catch (error) {
              if ([400, 404].includes(error.status)) changes.set(job.request_id, { status: "error", msg: "This archive has expired. Pack the website again for a fresh copy." });
              else failed = true;
            }
          }));
        }
        if (controller.signal.aborted) return;
        if (changes.size) setJobs(previous => previous.map(job => !isFinished(job) && changes.has(job.request_id) ? { ...job, ...changes.get(job.request_id) } : job));
        setPollingError(failed ? "Progress updates are temporarily unavailable. Retrying…" : "");
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 2000);
    }
    poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [hasActive]);

  return {
    jobs, notice: [storageError, pollingError].filter(Boolean).join(" "),
    update: data => setJobs(previous => previous.map(job => job.request_id === data.request_id ? { ...keep(data), updated_at: data.updated_at } : job)),
    add: job => setJobs(previous => trimHistory([{ ...keep(job), updated_at: job.updated_at }, ...previous])),
    clear: () => setJobs(previous => previous.filter(job => !isFinished(job))),
    expire: id => setJobs(previous => previous.map(job => job.request_id === id ? { ...job, status: "error", msg: "This archive has expired. Pack it again for a fresh copy." } : job)),
  };
}
