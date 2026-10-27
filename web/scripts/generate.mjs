// Generates the web workspace's types: one module per schema plus api.ts from the OpenAPI contract.

import path from "node:path";
import { fileURLToPath } from "node:url";
import { generate } from "../../scripts/generate-ts.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));

await generate({
  outDir: path.resolve(here, "../src/app/gen"),
  openapi: true,
});
