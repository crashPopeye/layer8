import { useEffect, useSyncExternalStore } from "react";
import { useNode } from "./react";

/** CSS token values by token name, e.g. {primary: "#a78bfa", radius: "0.5rem"}. */
export type Tokens = Record<string, string>;

/** Token overrides on top of the stylesheet: shared ones apply in both colour schemes. */
export interface Theme {
  shared?: Tokens;
  dark?: Tokens;
  light?: Tokens;
}

export type Scheme = "dark" | "light";
export type Mode = Scheme | "system";

/** The fields of the ui:theme node. */
export interface ThemeFields {
  theme: string;
  mode: Mode;
  accent: string;
  themes: Record<string, Theme>;
  builtin: string[];
}

/** The tokens the theme editor offers, with the ones that apply to both schemes. */
export const colorTokens = ["background", "foreground", "card", "primary", "primary-foreground", "muted", "muted-foreground", "border"];
export const sharedTokens = ["radius", "sans", "mono"];

/** Returns the colour scheme a mode stands for. */
export function resolveMode(mode: Mode, prefersDark: boolean): Scheme {
  return mode === "system" ? (prefersDark ? "dark" : "light") : mode;
}

/** Returns the tokens to set for a theme in one scheme, the accent overriding its primary colour. */
export function themeVars(theme: Theme, scheme: Scheme, accent = ""): Tokens {
  return { ...theme.shared, ...theme[scheme], ...(accent ? { primary: accent, ring: accent } : {}) };
}

const key = "layer8.theme";
let applied: string[] = [];

/** Sets the scheme and the token overrides on el, clearing the overrides it set before, and remembers them for the next start. */
export function applyTheme(el: HTMLElement, vars: Tokens, scheme: Scheme) {
  applied.forEach((name) => el.style.removeProperty(`--${name}`));
  Object.entries(vars).forEach(([name, value]) => el.style.setProperty(`--${name}`, value));
  applied = Object.keys(vars);
  el.dataset.theme = scheme;
  localStorage.setItem(key, JSON.stringify({ vars, scheme }));
}

/** Applies the theme remembered from the last run, dark with no overrides at first. */
export function restoreTheme() {
  let saved: { vars?: Tokens; scheme?: Scheme } = {};
  try {
    saved = JSON.parse(localStorage.getItem(key) ?? "{}");
  } catch {}
  applyTheme(document.documentElement, saved.vars ?? {}, saved.scheme === "light" ? "light" : "dark");
}

/** Returns the current value of a CSS token on the document. */
export function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim();
}

let preview: Theme | null = null;
const subscribers = new Set<() => void>();

/** Shows a theme in place of the saved one until it is cleared with null. */
export function setPreview(theme: Theme | null) {
  preview = theme;
  subscribers.forEach((s) => s());
}

/** Returns the theme being previewed, if any. */
export function usePreview(): Theme | null {
  return useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => preview,
  );
}

const darkQuery = "(prefers-color-scheme: dark)";

/** Reports whether the system prefers dark, re-rendering when that changes. */
export function usePrefersDark(): boolean {
  return useSyncExternalStore(
    (s) => {
      const q = matchMedia(darkQuery);
      q.addEventListener("change", s);
      return () => q.removeEventListener("change", s);
    },
    () => matchMedia(darkQuery).matches,
  );
}

/** Applies the ui:theme node, or the theme being previewed, to the document whenever either changes. */
export function useThemeSync() {
  const fields = useNode("ui:theme", 0).node?.fields as ThemeFields | undefined;
  const shown = usePreview();
  const prefersDark = usePrefersDark();
  useEffect(() => {
    if (!fields) return;
    const scheme = resolveMode(fields.mode, prefersDark);
    applyTheme(document.documentElement, themeVars(shown ?? fields.themes[fields.theme] ?? {}, scheme, fields.accent), scheme);
  }, [fields, shown, prefersDark]);
}
