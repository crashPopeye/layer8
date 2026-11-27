import { readFileSync } from "node:fs";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { panel } from "@/dock/registry";
import { commands } from "@/lib/palette";
import { mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { loadPlugin, type Importer, type Loaded } from "./loader";

const importer: Importer = (source) => import(/* @vite-ignore */ "data:text/javascript," + encodeURIComponent(source));
const sample = readFileSync("../examples/plugins/open-prs.js", "utf8");
const loaded: Loaded[] = [];

afterEach(() => loaded.splice(0).forEach((l) => l.dispose()));

async function load(name: string, source: string, client = mockClient()) {
  const l = await loadPlugin(name, source, client, importer);
  loaded.push(l);
  return l;
}

it("loads the sample plugin, whose panel renders through the SDK", async () => {
  const client = mockClient([{ target: "app", action: "add-project", args: { path: "/src/api" } }]);
  const l = await load("open-prs", sample, client);
  expect([l.panels, l.commands, l.error]).toEqual([["open-prs"], ["open-prs.show"], undefined]);
  const def = panel("open-prs")!;
  expect(def).toMatchObject({ title: "Open PRs", plugin: "open-prs", defaultPosition: "right" });
  expect(commands().find((c) => c.id === "open-prs.show")?.plugin).toBe("open-prs");

  render(
    <ClientProvider client={client}>
      <def.component />
    </ClientProvider>,
  );
  expect(await screen.findByText("No open pull requests.")).toBeTruthy();
});

it("removes what a plugin registered when it is disposed", async () => {
  const l = await load("open-prs", sample);
  l.dispose();
  expect(panel("open-prs")).toBeUndefined();
  expect(commands().some((c) => c.id === "open-prs.show")).toBe(false);
});

it("isolates a failing plugin, undoing what it registered before throwing", async () => {
  const source = `export default function (sdk) {
    sdk.registerPanel({ id: "half", title: "Half", component: () => null });
    throw new Error("boom");
  }`;
  expect(await load("half", source)).toMatchObject({ panels: [], commands: [], error: "boom" });
  expect(panel("half")).toBeUndefined();
  expect((await load("bad", "export default 42")).error).toMatch(/default export must be a function/);
  expect((await load("syntax", "export default (")).error).toBeTruthy();
});

it("acts on the daemon as the plugin", async () => {
  const client = mockClient();
  const actors: (string | undefined)[] = [];
  const spy = { ...client, doOps: (ops: Parameters<typeof client.doOps>[0], actor?: string) => (actors.push(actor), client.doOps(ops)) };
  const source = `export default (sdk) => { sdk.do([{ target: "app", action: "add-project", args: { path: "/src/x" } }]) }`;
  await load("adder", source, spy);
  expect(actors).toEqual(["plugin:adder"]);
});
