import { X } from "lucide-react";
import { useEffect, useSyncExternalStore } from "react";
import { useClient } from "@/lib/react";
import { cn } from "@/lib/utils";

interface Toast {
  id: number;
  level: string;
  text: string;
}

let toasts: Toast[] = [];
let next = 0;
const subscribers = new Set<() => void>();

function set(list: Toast[]) {
  toasts = list;
  subscribers.forEach((s) => s());
}

function dismiss(id: number) {
  set(toasts.filter((t) => t.id !== id));
}

/** Shows a notification for a few seconds; level is info, success, warning or error. */
export function toast(level: string, text: string) {
  const id = next++;
  set([...toasts, { id, level, text }]);
  setTimeout(() => dismiss(id), level === "error" ? 10_000 : 5_000);
}

const tone: Record<string, string> = { success: "border-ok/50", warning: "border-wait/50", error: "border-bad/50" };

/** The stack of notifications in the bottom-right corner, fed by `toast` and the daemon's toast events. */
export function Toasts() {
  const client = useClient();
  const list = useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => toasts,
  );
  useEffect(() => client.subscribe((e) => e.type === "toast" && toast(e.data.level, e.data.text)), [client]);
  return (
    <div className="fixed right-3 bottom-9 z-50 grid w-80 gap-2">
      {list.map((t) => (
        <div key={t.id} role="status" className={cn("flex gap-2 rounded-md border bg-popover p-3 text-[12px] shadow-lg", tone[t.level])}>
          <p className="flex-1 break-words whitespace-pre-wrap">{t.text}</p>
          <button className="self-start text-muted-foreground hover:text-foreground" onClick={() => dismiss(t.id)} aria-label="Dismiss">
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}
