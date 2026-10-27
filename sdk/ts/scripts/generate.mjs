// Generates the SDK's plugin protocol types and an index that exposes each module as a namespace.

import path from "node:path";
import { fileURLToPath } from "node:url";
import { generate } from "../../../scripts/generate-ts.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));

/** Schemas that make up the plugin protocol. */
const protocolSchemas = ["request", "event", "check-result", "plugin", "messages"];

await generate({
  outDir: path.resolve(here, "../src/gen"),
  schemas: protocolSchemas,
  index: true,
});
