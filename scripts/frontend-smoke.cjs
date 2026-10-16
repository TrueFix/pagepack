// Run: PLAYWRIGHT_BROWSERS_PATH=/tmp/opencode/pagepack-browsers npm exec --package=@playwright/test -- node scripts/frontend-smoke.cjs
const { createServer } = require("node:http");
const { readFile } = require("node:fs/promises");
const path = require("node:path");
const assert = require("node:assert/strict");
// Resolve the temporary npm-exec package without adding browser tooling to the app.
const bin = process.env.PATH.split(path.delimiter).find(p => p.includes("_npx") && p.endsWith(".bin"));
const { chromium } = bin ? require(path.resolve(bin, "../playwright")) : require("playwright");
const root = path.resolve(__dirname, "..");
const errors = [];
let polls = 0;
let holdJob = false;
let canceled = false;
const fixture = { request_id: "3d809ef3-acd2-4904-8626-c6201e60ab49", url: "https://example.com", status: "pending", created_at: new Date().toISOString() };
// Mirrors the v1 API shapes: live progress while cloning, then result + download when done.
const cloning = { ...fixture, status: "cloning", updated_at: "1", summary: "Downloading", progress: { downloaded: "1.2 MB", downloaded_bytes: 1258291, files: 12, rate: "600 KB/s", limit_percent: 1.2, stage_percent: null, requests_failed: 0, last_url: "https://example.com/style.css" } };
const done = { ...fixture, status: "done", updated_at: "2", summary: "Ready", result: { archive_size: "410 KB", archive_bytes: 419840, site_size: "1.2 MB", files: 12, space_saved_percent: 67, duration: "4.2s", requests_failed: 0 }, download: { url: "https://downloads.example/archive.zip", filename: "example.com.zip" }, download_url: "https://downloads.example/archive.zip" };
const server = createServer(async (req, res) => {
  res.setHeader("Content-Type", "application/json");
  if (req.url === "/api/v1/config") return res.end(JSON.stringify({ max_website_bytes: 104857600 }));
  if (req.url === "/api/v1/jobs" && req.method === "POST") { polls = 0; res.writeHead(202); return res.end(JSON.stringify({ ...fixture, updated_at: "0" })); }
  if (req.url === `/api/v1/jobs/${fixture.request_id}/cancel` && req.method === "POST") {
    canceled = true;
    return res.end(JSON.stringify({ ...fixture, status: "canceled", updated_at: "3", summary: "Canceled. Files removed." }));
  }
  if (req.url === `/api/v1/jobs/${fixture.request_id}`) return res.end(JSON.stringify(canceled ? { ...fixture, status: "canceled", updated_at: "3" } : holdJob || ++polls < 2 ? cloning : done));
  const pathname = new URL(req.url, "http://localhost").pathname;
  const filename = pathname === "/" ? "index.html" : pathname === "/mark.svg" || pathname.startsWith("/assets/") ? pathname.slice(1) : null;
  if (!filename || filename.includes("..")) { res.writeHead(404); return res.end(); }
  res.setHeader("Content-Type", filename.endsWith(".css") ? "text/css" : filename.endsWith(".js") ? "text/javascript" : filename.endsWith(".svg") ? "image/svg+xml" : filename.endsWith(".woff2") ? "font/woff2" : "text/html");
  try { res.end(await readFile(path.join(root, "dist", filename))); } catch { res.writeHead(404); res.end(); }
});
(async () => {
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const page = await context.newPage();
    page.on("pageerror", err => errors.push(err.message));
    await page.goto(base);
    await page.locator("#submit:enabled").waitFor();
    await page.evaluate(() => document.fonts.ready);
    assert.match(await page.locator("#limit").textContent(), /100 MB/);
    await page.getByRole("link", { name: "Try a simple page" }).click();
    assert.equal(await page.locator("#website").inputValue(), "https://example.com");
    await page.getByRole("button", { name: "New archive", exact: true }).click();
    assert.equal(await page.locator("#website").inputValue(), "");
    await page.locator("#website").fill("not-a-url");
    await page.locator("#submit").click();
    assert.match(await page.locator("#form-message").textContent(), /valid HTTP or HTTPS/);
    assert.equal(await page.locator(".archive-row").count(), 0);
    await page.getByRole("button", { name: "New archive", exact: true }).click();
    await page.screenshot({ path: path.join(root, ".preview-desktop.png"), fullPage: true });
    await page.locator("#website").fill("https://example.com");
    await page.locator("#submit").click();
    await page.locator(".archive-row").waitFor();
    await page.locator('[data-status="done"]').waitFor({ timeout: 15000 });
    const saved = await page.evaluate(() => JSON.parse(localStorage.getItem("pagepack.jobs.v1")));
    assert.equal(saved.length, 1);
    assert.equal(saved[0].status, "done");
    assert.equal(saved[0].download_url, undefined);
    assert.equal(saved[0].download, undefined);
    assert.equal(saved[0].result.archive_size, "410 KB");
    assert.match(await page.locator(".archive-detail").first().textContent(), /410 KB ZIP · 12 files · 67% smaller · 4.2s/);
    await page.locator('[data-filter="active"]').click();
    assert.equal(await page.locator(".archive-row").count(), 0);
    assert.equal(await page.locator("#empty-state").isVisible(), true);
    await page.locator('[data-filter="done"]').click();
    assert.equal(await page.locator(".archive-row").count(), 1);
    await page.reload();
    await page.locator("#submit:enabled").waitFor();
    assert.equal(await page.locator(".archive-row").count(), 1);
    await page.screenshot({ path: path.join(root, ".preview-populated.png"), fullPage: true });
    for (const width of [320, 375, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      const dimensions = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
      if (dimensions.scroll > dimensions.width) console.log(await page.evaluate(() => [...document.querySelectorAll("body *")].filter(el => el.getBoundingClientRect().right > innerWidth + 1).map(el => ({tag: el.tagName, class: el.className, right: el.getBoundingClientRect().right})).slice(0, 20)));
      assert.ok(dimensions.scroll <= dimensions.width, `horizontal overflow at ${width}: ${JSON.stringify(dimensions)}`);
      const stage = await page.locator(".archive-stage").boundingBox();
      const action = await page.locator(".archive-action").boundingBox();
      assert.ok(stage.x + stage.width <= action.x || stage.y + stage.height <= action.y, `archive controls overlap at ${width}px`);
      if (width === 375) {
        await page.getByRole("button", { name: "Open menu", exact: true }).click();
        const menu = page.getByRole("dialog");
        await menu.waitFor();
        await menu.getByRole("link", { name: "Your archives", exact: true }).click();
        await menu.waitFor({ state: "hidden" });
        await page.screenshot({ path: path.join(root, ".preview-mobile.png"), fullPage: true });
      }
    }
    const other = await browser.newContext();
    const otherPage = await other.newPage();
    await otherPage.goto(base);
    await otherPage.locator("#submit:enabled").waitFor();
    assert.equal(await otherPage.locator(".archive-row").count(), 0);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.locator("#clear-history").click();
    assert.equal(await page.locator(".archive-row").count(), 0);
    assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem("pagepack.jobs.v1")).length), 0);
    await page.evaluate(fixture => localStorage.setItem("pagepack.jobs.v1", JSON.stringify([{ ...fixture, status: "error", msg: "Website exceeded its size limit" }])), fixture);
    await page.reload();
    await page.locator("#submit:enabled").waitFor();
    await page.locator('[data-filter="error"]').click();
    assert.equal(await page.locator(".archive-row").count(), 1);
    await page.getByRole("button", { name: "Pack example.com again" }).click();
    assert.equal(await page.locator("#website").inputValue(), "https://example.com");
    holdJob = true;
    await page.evaluate(fixture => localStorage.setItem("pagepack.jobs.v1", JSON.stringify([{ ...fixture, status: "cloning" }])), fixture);
    await page.reload();
    await page.getByRole("button", { name: "Cancel archive of example.com" }).click();
    await page.locator('[data-status="canceled"]').waitFor();
    assert.match(await page.locator(".archive-detail").textContent(), /files cleaned up/);
    assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem("pagepack.jobs.v1"))[0].status), "canceled");
    await page.locator('[data-filter="active"]').click();
    assert.equal(await page.locator(".archive-row").count(), 0);
    await page.locator('[data-filter="error"]').click();
    assert.equal(await page.locator(".archive-row").count(), 1);
    await page.locator("#clear-history").click();
    assert.equal(await page.locator(".archive-row").count(), 0);
    assert.deepEqual(errors, []);
    console.log("PASS: submission, polling, local persistence, filters, clearing, and responsive widths 320–1440px.");
    await other.close();
    await context.close();
  } finally { await browser.close(); server.close(); }
})().catch(err => { console.error(err); server.close(); process.exitCode = 1; });
