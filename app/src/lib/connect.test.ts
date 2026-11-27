import { afterEach, expect, it, vi } from "vitest";
import { connect } from "./connect";

afterEach(() => {
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

it("uses the daemon when /api/version answers, and the mock otherwise", async () => {
  history.replaceState(null, "", "/?url=http://127.0.0.1:7979&token=s3cret");
  const fetch = vi.fn(async (_url: URL) => new Response('{"version":"dev"}'));
  vi.stubGlobal("fetch", fetch);
  expect((await connect()).mode).toBe("daemon");
  expect(String(fetch.mock.calls[0][0])).toBe("http://127.0.0.1:7979/api/version");

  vi.stubGlobal("fetch", vi.fn(async () => Promise.reject(new TypeError("refused"))));
  expect((await connect()).mode).toBe("mock");
});
