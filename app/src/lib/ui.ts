import type { SerializedDockview } from "dockview-react";
import { applyPreset, openPanel } from "@/dock/Dock";
import type { Slot } from "@/dock/registry";
import { setFocus } from "./focus";
import type { Client, UiCommand } from "./types";

/** Applies a window command from a `ui` event, looking up a session's task before focusing it. */
export function applyUi(cmd: UiCommand, client: Client) {
  if (cmd.verb === "open-panel" && typeof cmd.panel === "string") openPanel(cmd.panel, cmd.position as Slot | undefined);
  if (cmd.verb === "preset" && typeof cmd.name === "string") applyPreset(cmd.name, cmd.layout as SerializedDockview | undefined);
  if (cmd.verb !== "focus" || typeof cmd.addr !== "string") return;
  const addr = cmd.addr;
  if (!addr.startsWith("session:")) return setFocus(addr);
  client.getNode(addr, 0).then((n) => setFocus(addr, n.fields?.task as string), () => {});
}
