import { expect, it } from "vitest";
import { applyTheme, resolveMode, themeVars } from "./theme";

it("merges shared tokens, the scheme's tokens and the accent", () => {
  const theme = { shared: { radius: "0", primary: "#111" }, dark: { primary: "#222", background: "#000" }, light: { background: "#fff" } };
  expect(themeVars(theme, "dark")).toEqual({ radius: "0", primary: "#222", background: "#000" });
  expect(themeVars(theme, "light", "#f06")).toEqual({ radius: "0", primary: "#f06", ring: "#f06", background: "#fff" });
  expect(themeVars({}, "dark")).toEqual({});
});

it("resolves system mode from the system preference", () => {
  expect(resolveMode("system", true)).toBe("dark");
  expect(resolveMode("system", false)).toBe("light");
  expect(resolveMode("light", true)).toBe("light");
});

it("applies a theme, clearing the tokens of the previous one", () => {
  const el = document.createElement("div");
  applyTheme(el, { primary: "#f06", radius: "0" }, "light");
  expect(el.dataset.theme).toBe("light");
  expect(el.style.getPropertyValue("--primary")).toBe("#f06");

  applyTheme(el, { background: "#000" }, "dark");
  expect(el.style.getPropertyValue("--primary")).toBe("");
  expect(el.style.getPropertyValue("--radius")).toBe("");
  expect(el.style.getPropertyValue("--background")).toBe("#000");
  expect(JSON.parse(localStorage.getItem("layer8.theme")!)).toEqual({ vars: { background: "#000" }, scheme: "dark" });
});
