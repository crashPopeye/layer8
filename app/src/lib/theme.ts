export type Theme = "dark" | "light";

const key = "layer8.theme";

/** Returns the stored theme, dark by default. */
export function storedTheme(): Theme {
  return localStorage.getItem(key) === "light" ? "light" : "dark";
}

/** Applies a theme to the document and remembers it. */
export function setTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem(key, theme);
}
