import { Droplets, Link2, Package } from "lucide-react";

function Token({ icon: Icon, hue, children }) {
  return (
    <span className="pane mr-[0.06em] ml-[0.2em] inline-flex translate-y-[-0.08em] items-center gap-[0.25em] rounded-full py-[0.08em] pr-[0.4em] pl-[0.12em] align-middle whitespace-nowrap">
      <span aria-hidden="true" className="grid size-[0.95em] place-items-center rounded-full text-white" style={{ background: `radial-gradient(circle at 35% 28%, oklch(0.92 0.08 ${hue}), oklch(0.62 0.17 ${hue}) 75%)`, boxShadow: `inset 0 1px 0 oklch(1 0 0 / 60%), 0 6px 14px -6px oklch(0.6 0.17 ${hue} / 70%)` }}><Icon className="size-[0.5em]" strokeWidth={2.25} /></span>
      {children}
    </span>
  );
}

const NOTES = [
  { n: "01", title: "Paste", text: "Any public HTTP or HTTPS address. Pagepack follows its pages and gathers the styles, images and files they link to." },
  { n: "02", title: "Watch it fill", text: "Each archive is a vessel on your shelf: queued, cloning, uploading, ready. The list lives only in this browser." },
  { n: "03", title: "Take it home", text: "Grab a fresh, time-limited link to the ZIP. Pages behind a login or built by JavaScript may not come along." },
];

export default function Notes() {
  return (
    <section id="how-it-works" aria-labelledby="how-heading" className="scroll-mt-8 px-4 py-24 sm:px-8 lg:px-28">
      <div className="mx-auto max-w-5xl">
        <p className="etched font-mono text-[11px] tracking-[0.16em] uppercase">03 — Field notes</p>
        <h2 id="how-heading" className="font-display mt-6 max-w-4xl text-[clamp(2rem,4.6vw,3.9rem)] leading-[1.12] font-semibold tracking-[-0.045em] text-balance">
          Paste a <Token icon={Link2} hue={250}>link</Token>, watch the glass <Token icon={Droplets} hue={165}>fill</Token>, then carry the <Token icon={Package} hue={40}>ZIP</Token> home.
        </h2>
        <dl className="mt-16 grid gap-5 sm:grid-cols-3">
          {NOTES.map(({ n, title, text }, i) => (
            <div key={n} className="slab streak rounded-[28px] p-6" style={{ "--cast": `oklch(0.75 0.13 ${[250, 165, 40][i]} / 35%)` }}>
              <dt className="flex items-baseline justify-between">
                <span className="font-display text-xl font-semibold tracking-[-0.03em]">{title}</span>
                <span className="etched font-mono text-[11px]">{n}</span>
              </dt>
              <dd className="text-muted-foreground mt-3 text-sm leading-relaxed">{text}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}
