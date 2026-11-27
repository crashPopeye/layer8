import { useEffect, useState, useSyncExternalStore, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Input, NativeSelect, Textarea } from "@/components/ui/input";
import { setFocus } from "@/lib/focus";
import { useClient } from "@/lib/react";
import { argsFrom, fieldsOf, type Values } from "@/lib/schema";
import type { Action, Addr } from "@/lib/types";

/** A form for one action's args, generated from its JSON Schema. */
export function ActionForm({
  action,
  onSubmit,
}: {
  action: Action;
  onSubmit: (args: Record<string, unknown>) => Promise<string | null>;
}) {
  const fields = fieldsOf(action.args);
  const [values, setValues] = useState<Values>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (name: string, v: string | boolean) => setValues((old) => ({ ...old, [name]: v }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    const built = argsFrom(fields, values);
    if ("error" in built) return setError(built.error);
    setBusy(true);
    setError(await onSubmit(built.args).finally(() => setBusy(false)));
  }

  return (
    <form onSubmit={submit} className="grid gap-3">
      {fields.map((f, i) => (
        <label key={f.name} className="grid gap-1 text-[12px]">
          <span className="text-muted-foreground">
            {f.name}
            {f.required && <span className="text-primary"> *</span>}
          </span>
          {f.kind === "boolean" ? (
            <input
              type="checkbox"
              className="size-4 accent-primary"
              checked={values[f.name] === true}
              onChange={(e) => set(f.name, e.target.checked)}
            />
          ) : f.kind === "enum" ? (
            <NativeSelect value={String(values[f.name] ?? "")} onChange={(e) => set(f.name, e.target.value)} autoFocus={i === 0}>
              <option value="" />
              {f.options!.map((o) => (
                <option key={String(o)}>{String(o)}</option>
              ))}
            </NativeSelect>
          ) : f.kind === "json" || f.name === "content" ? (
            <Textarea
              value={String(values[f.name] ?? "")}
              placeholder={f.description}
              onChange={(e) => set(f.name, e.target.value)}
              autoFocus={i === 0}
            />
          ) : (
            <Input
              type={f.kind === "number" ? "number" : "text"}
              value={String(values[f.name] ?? "")}
              placeholder={f.description}
              onChange={(e) => set(f.name, e.target.value)}
              autoFocus={i === 0}
            />
          )}
        </label>
      ))}
      {error && <p className="text-[12px] text-bad">{error}</p>}
      <Button type="submit" size="sm" disabled={busy} autoFocus={fields.length === 0}>
        {action.name}
      </Button>
    </form>
  );
}

interface Request {
  targets: Addr[];
  action: string;
}

let request: Request | null = null;
const subscribers = new Set<() => void>();

/** Opens the action dialog for action on the first of targets, the others offered as alternatives. */
export function openAction(targets: Addr | Addr[], action: string) {
  request = { targets: ([] as Addr[]).concat(targets), action };
  subscribers.forEach((s) => s());
}

function closeAction() {
  request = null;
  subscribers.forEach((s) => s());
}

/** The one dialog that runs an action picked from a menu or a shortcut, refocusing on the node it creates. */
export function ActionDialog() {
  const req = useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => request,
  );
  return (
    <Dialog open={!!req} onOpenChange={(open) => !open && closeAction()}>
      <DialogContent>{req && <ActionRequest key={req.targets.join() + req.action} req={req} />}</DialogContent>
    </Dialog>
  );
}

function ActionRequest({ req }: { req: Request }) {
  const client = useClient();
  const [target, setTarget] = useState(req.targets[0]);
  const [action, setAction] = useState<Action | null | undefined>(undefined);

  useEffect(() => {
    setAction(undefined);
    client.getNode(target, 0).then(
      (n) => setAction(n.actions.find((a) => a.name === req.action) ?? null),
      () => setAction(null),
    );
  }, [client, target, req.action]);

  async function run(args: Record<string, unknown>) {
    const [r] = await client.doOps([{ target, action: req.action, args }]);
    if (!r.ok) return r.message ?? `${req.action} failed`;
    if (r.addr !== target) setFocus(r.addr);
    closeAction();
    return null;
  }

  return (
    <>
      <DialogTitle className="font-mono text-sm">{req.action}</DialogTitle>
      <DialogDescription className="text-[12px] text-muted-foreground">{action?.help ?? target}</DialogDescription>
      {req.targets.length > 1 && (
        <NativeSelect value={target} onChange={(e) => setTarget(e.target.value)} aria-label="target">
          {req.targets.map((t) => (
            <option key={t}>{t}</option>
          ))}
        </NativeSelect>
      )}
      {action === null && (
        <p className="text-[12px] text-bad">
          {target} offers no {req.action} right now.
        </p>
      )}
      {action && <ActionForm key={target} action={action} onSubmit={run} />}
    </>
  );
}
