import { openAction } from "@/components/action-form";
import { crumbTrail } from "./crumbs";
import { getFocus, taskOf } from "./focus";
import type { Client } from "./types";

/** Opens the new-task form on the focused node's project, offering the other projects too. */
export async function newTask(client: Client) {
  const projects = (await client.getNode("app", 1)).children.map((c) => c.addr);
  const [top] = await crumbTrail(client.getNode, getFocus()).catch(() => []);
  const mine = projects.find((p) => p === top?.addr);
  if (projects.length) openAction(mine ? [mine, ...projects.filter((p) => p !== mine)] : projects, "create-task");
}

/** Opens the new-sub-task form on the focused task. */
export function newSubtask() {
  const task = taskOf(getFocus());
  if (task) openAction(task, "create-subtask");
}
