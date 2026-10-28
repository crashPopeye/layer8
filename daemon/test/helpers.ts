import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Must run before config.ts is imported anywhere.
export const testHome = mkdtempSync(join(tmpdir(), "layer8-test-"));
process.env.LAYER8_HOME = testHome;
