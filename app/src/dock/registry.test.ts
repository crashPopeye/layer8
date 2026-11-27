import { FileText } from "lucide-react";
import { expect, it } from "vitest";
import { panel, panels, placement, registerPanel } from "./registry";

it("registers panels by id, a second registration replacing the first", () => {
  const component = () => null;
  registerPanel({ id: "pr", title: "PR", icon: FileText, component, defaultPosition: "right" });
  registerPanel({ id: "diff", title: "Diff", icon: FileText, component, defaultPosition: "right-bottom" });
  registerPanel({ id: "pr", title: "Pull request", icon: FileText, component, defaultPosition: "right" });
  expect(panels().map((p) => p.id)).toEqual(["pr", "diff"]);
  expect(panel("pr")?.title).toBe("Pull request");
});

it("places a panel as a tab of its slot, else beside the nearest neighbour", () => {
  expect(placement("left", {})).toBeUndefined();
  expect(placement("left", { center: "C" })).toEqual({ referenceGroup: "C", direction: "left" });
  expect(placement("right", { center: "C", left: "L" })).toEqual({ referenceGroup: "C", direction: "right" });
  expect(placement("right-bottom", { center: "C", right: "R" })).toEqual({ referenceGroup: "R", direction: "below" });
  expect(placement("right", { "right-bottom": "B" })).toEqual({ referenceGroup: "B", direction: "above" });
  expect(placement("center", { center: "C" })).toEqual({ referenceGroup: "C" });
});
