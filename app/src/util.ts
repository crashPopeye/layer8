import { useEffect } from "react";
import DOMPurify from "dompurify";
import { marked } from "marked";

export const markdown = (src: string) => DOMPurify.sanitize(marked.parse(src, { async: false }) as string);

export const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour12: false });

export const home = (p: string) => p.replace(/^\/(Users|home)\/[^/]+/, "~");

export const usd = (n: number) => `$${n.toFixed(2)}`;

const typing = (el: EventTarget | null) =>
  el instanceof HTMLElement && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

/** Single-key shortcuts, ignored while the user is typing. */
export function useKeys(map: Record<string, () => void>, enabled = true): void {
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      const fn = map[e.key.toLowerCase()];
      if (fn) {
        e.preventDefault();
        fn();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
}
