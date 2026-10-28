import { useEffect } from "/vendor/preact-htm.js";

export const clock = (iso) => (iso ? new Date(iso).toLocaleTimeString([], { hour12: false }) : "");
export const day = (iso) => (iso ? new Date(iso).toLocaleDateString([], { month: "short", day: "numeric" }) : "");
export const home = (p) => (p || "").replace(/^\/Users\/[^/]+/, "~");
export const usd = (n) => `$${(n || 0).toFixed(2)}`;
export const cls = (...xs) => xs.filter(Boolean).join(" ");

export function ago(iso) {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

const typing = (el) =>
  el instanceof HTMLElement && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

/** Single-key shortcuts, ignored while typing or when a modifier is held. */
export function useKeys(map, enabled = true) {
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e) => {
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

/** Esc closes a sheet wherever focus is. */
export function useEscape(onClose) {
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
}

/** Focus an element once it is mounted (autofocus only works on page load). */
export function useFocus(ref) {
  useEffect(() => {
    const t = setTimeout(() => ref.current && ref.current.focus(), 30);
    return () => clearTimeout(t);
  }, []);
}

export const statusLabel = (s) => (s || "").replace(/_/g, " ").toLowerCase();

// Slice summary for chips: one status square per concern.
export function prSquare(x) {
  if (x.prState === "MERGED") return "MERGED";
  if (x.prState === "CLOSED") return "CLOSED";
  if (!x.prNumber) return "none";
  return x.ciState || "PENDING";
}
