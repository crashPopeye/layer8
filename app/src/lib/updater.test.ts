import { beforeEach, expect, it, vi } from "vitest";

const tauri = vi.hoisted(() => ({
  isTauri: vi.fn(() => true),
  check: vi.fn(),
  relaunch: vi.fn(async () => {}),
}));
vi.mock("@tauri-apps/api/core", () => ({ isTauri: tauri.isTauri, invoke: vi.fn() }));
vi.mock("@tauri-apps/plugin-updater", () => ({ check: tauri.check }));
vi.mock("@tauri-apps/plugin-process", () => ({ relaunch: tauri.relaunch }));
vi.mock("./connect", () => ({ daemonInfo: async () => ({ url: "http://127.0.0.1:7979", token: "s3cret" }) }));

import { checkForUpdate, dismissUpdate, installUpdate, useUpdate } from "./updater";
import { renderHook } from "@testing-library/react";

const state = () => renderHook(() => useUpdate()).result.current;

beforeEach(() => {
  dismissUpdate();
  vi.clearAllMocks();
});

it("checks the daemon feed with the daemon token, then installs and relaunches", async () => {
  const update = { version: "0.2.0", currentVersion: "0.1.0", downloadAndInstall: vi.fn(async () => {}) };
  tauri.check.mockResolvedValue(update);
  await checkForUpdate();
  expect(tauri.check).toHaveBeenCalledWith({ headers: { Authorization: "Bearer s3cret" } });
  expect(state().update).toBe(update);

  await installUpdate();
  expect(update.downloadAndInstall).toHaveBeenCalled();
  expect(tauri.relaunch).toHaveBeenCalled();
});

it("stays quiet on launch without an update, and says so when asked", async () => {
  tauri.check.mockResolvedValue(null);
  await checkForUpdate();
  expect(state()).toEqual({});
  await checkForUpdate(true);
  expect(state().message).toBe("layer8 is up to date.");

  tauri.check.mockRejectedValue(new Error("offline"));
  dismissUpdate();
  await checkForUpdate();
  expect(state()).toEqual({});
  await checkForUpdate(true);
  expect(state().message).toMatch(/failed: Error: offline/);
});
