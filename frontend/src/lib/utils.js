import { clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs) { return twMerge(clsx(inputs)); }

/** Stable hue (0–359) for a string, so every website gets its own glass tint. */
export function hueOf(text = "") { let h = 7; for (const c of text) h = (h * 31 + c.charCodeAt(0)) % 360; return h; }

export function hostOf(url) { try { return new URL(url).host; } catch { return url; } }
