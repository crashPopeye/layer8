import { expect, it } from "vitest";
import { languageOf, viewerFor } from "./kind";

it("picks a viewer from the name and the served media type", () => {
  expect(viewerFor("05-plan.md", "text/markdown; charset=utf-8")).toBe("markdown");
  expect(viewerFor("notes.markdown", "application/octet-stream")).toBe("markdown");
  expect(viewerFor("mockup.html", "text/html; charset=utf-8")).toBe("html");
  expect(viewerFor("diagram.svg", "image/svg+xml")).toBe("image");
  expect(viewerFor("shot.png", "image/png")).toBe("image");
  expect(viewerFor("main.go", "text/plain; charset=utf-8")).toBe("code");
  expect(viewerFor("package.json", "application/json")).toBe("code");
  expect(viewerFor("app.js", "text/javascript; charset=utf-8")).toBe("code");
  expect(viewerFor("feed.atom", "application/atom+xml")).toBe("code");
  expect(viewerFor("bin.zip", "application/zip")).toBe("binary");
  expect(viewerFor("blob", "application/octet-stream")).toBe("binary");
});

it("names the highlighting language", () => {
  expect(languageOf("internal/http/ratelimit.go")).toBe("go");
  expect(languageOf("Makefile")).toBe("make");
  expect(languageOf("go.mod")).toBe("go");
  expect(languageOf("LICENSE")).toBe("text");
});
