import { EventEmitter } from "node:events";
import type { SessionEvent } from "./types.js";

type Events = {
  changed: [];
  event: [SessionEvent];
};

/** In-process fan-out: the server forwards both to every connected app window. */
export const bus = new EventEmitter<Events>();
