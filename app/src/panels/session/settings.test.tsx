import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { families, ModelControl, settingsLabel, versions, type Model, type Settings } from "./settings";

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

afterEach(cleanup);

const models: Model[] = [
  { value: "opus", displayName: "Opus", efforts: ["low", "high"] },
  { value: "opus[1m]", displayName: "Opus (1M context)", efforts: ["low", "high"] },
  { value: "sonnet", displayName: "Sonnet" },
  { value: "sonnet[1m]", displayName: "Sonnet (1M context)" },
  { value: "haiku", displayName: "Haiku", efforts: [] },
];

const base: Settings = { harness: "claude", model: "", effort: "", permissionMode: "" };

it("groups model variants into families and versions", () => {
  expect(families(models)).toEqual([
    ["opus", "Opus"],
    ["sonnet", "Sonnet"],
    ["haiku", "Haiku"],
  ]);
  expect(versions(models, "opus[1m]")).toEqual([
    ["opus", "standard"],
    ["opus[1m]", "1M"],
  ]);
  expect(versions(models, "haiku")).toEqual([]);
});

it("summarises settings in one line, leaving defaults out", () => {
  expect(settingsLabel(base, models)).toBe("Default model");
  expect(settingsLabel({ ...base, model: "opus[1m]", effort: "high", permissionMode: "plan" }, models)).toBe("Opus (1M context) · high · plan");
  expect(settingsLabel({ ...base, model: "gpt-9" }, [])).toBe("gpt-9");
});

it("changes the model family keeping the chosen version", () => {
  const onChange = vi.fn();
  render(<ModelControl settings={{ ...base, model: "opus[1m]" }} models={models} harnesses={[]} onChange={onChange} />);
  fireEvent.click(screen.getByTitle("Model settings"));

  expect(screen.queryByRole("radiogroup", { name: "Harness" })).toBeNull();
  expect(screen.getByRole("radio", { name: "1M" }).getAttribute("aria-checked")).toBe("true");

  fireEvent.click(screen.getByRole("radio", { name: "Sonnet" }));
  expect(onChange).toHaveBeenLastCalledWith({ model: "sonnet[1m]" });

  fireEvent.click(screen.getByRole("radio", { name: "plan" }));
  expect(onChange).toHaveBeenLastCalledWith({ permissionMode: "plan" });
});

it("offers the harness only when there is a choice, and hides effort for models without levels", () => {
  const onChange = vi.fn();
  render(<ModelControl settings={{ ...base, model: "haiku" }} models={models} harnesses={["claude", "codex"]} onChange={onChange} />);
  fireEvent.click(screen.getByTitle("Model settings"));

  expect(screen.queryByRole("radiogroup", { name: "Effort" })).toBeNull();
  fireEvent.click(screen.getByRole("radio", { name: "codex" }));
  expect(onChange).toHaveBeenLastCalledWith({ harness: "codex", model: "", effort: "" });
});
