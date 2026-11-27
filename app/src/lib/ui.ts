import { openPanel } from "@/dock/Dock";
import { setFocus } from "./focus";
import type { UiCommand } from "./types";

/** Applies a window command from a `ui` event. */
export function applyUi(cmd: UiCommand) {
  if (cmd.verb === "open-panel" && typeof cmd.panel === "string") openPanel(cmd.panel);
  if (cmd.verb === "focus" && typeof cmd.addr === "string") setFocus(cmd.addr);
}
