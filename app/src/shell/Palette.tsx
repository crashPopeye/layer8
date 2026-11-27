import { Command } from "cmdk";
import { useEffect, useState, useSyncExternalStore } from "react";
import { openAction } from "@/components/action-form";
import { flushLayout, openPanel } from "@/dock/Dock";
import { usePanels } from "@/dock/registry";
import { setFocus, useFocus } from "@/lib/focus";
import { buildPalette, commands, paletteNodes, type Item } from "@/lib/palette";
import { useClient } from "@/lib/react";
import { toast } from "./Toasts";

let open = false;
const subscribers = new Set<() => void>();

/** Opens or closes the ⌘K palette. */
export function setPaletteOpen(next: boolean) {
  open = next;
  subscribers.forEach((s) => s());
}

/** Toggles the ⌘K palette. */
export function togglePalette() {
  setPaletteOpen(!open);
}

/** The ⌘K palette: everything the window can do, built from the tree around the focused node. */
export function Palette() {
  const client = useClient();
  const focus = useFocus();
  const panels = usePanels();
  const isOpen = useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => open,
  );
  const [items, setItems] = useState<Item[]>([]);

  useEffect(() => {
    if (!isOpen) return;
    let live = true;
    paletteNodes(client, focus).then((nodes) => {
      if (!live) return;
      setItems(
        buildPalette(
          { ...nodes, panels, commands: commands() },
          {
            focus: setFocus,
            openAction,
            openPanel,
            run: (op) =>
              client.doOps([op]).then(([r]) => !r.ok && toast("error", r.message ?? `${op.action} failed`)),
            savePreset: () => flushLayout().then(() => openAction("ui:layout", "save-preset")),
          },
        ),
      );
    });
    return () => {
      live = false;
    };
  }, [client, focus, panels, isOpen]);

  const groups = [...new Set(items.map((i) => i.group))];
  return (
    <Command.Dialog
      open={isOpen}
      onOpenChange={setPaletteOpen}
      label="Command palette"
      overlayClassName="fixed inset-0 z-50 bg-black/40"
      contentClassName="fixed top-[14%] left-1/2 z-50 w-[560px] -translate-x-1/2 overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-xl"
    >
      <Command.Input
        placeholder="Go to, run an action, switch layout or theme…"
        className="h-11 w-full border-b bg-transparent px-3 text-[13px] outline-none placeholder:text-muted-foreground"
      />
      <Command.List className="max-h-[50vh] overflow-auto p-1 text-[13px]">
        <Command.Empty className="p-3 text-muted-foreground">Nothing matches.</Command.Empty>
        {groups.map((g) => (
          <Command.Group
            key={g}
            heading={g}
            className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:text-muted-foreground"
          >
            {items
              .filter((i) => i.group === g)
              .map((i) => (
                <Command.Item
                  key={i.id}
                  value={i.id}
                  keywords={[i.title, i.hint ?? "", g]}
                  onSelect={() => {
                    setPaletteOpen(false);
                    i.run();
                  }}
                  className="flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 data-[selected=true]:bg-accent"
                >
                  <span className="truncate">{i.title}</span>
                  {i.hint && <span className="ml-auto truncate text-[11px] text-muted-foreground">{i.hint}</span>}
                </Command.Item>
              ))}
          </Command.Group>
        ))}
      </Command.List>
    </Command.Dialog>
  );
}
