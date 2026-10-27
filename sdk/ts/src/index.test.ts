import { describe, expect, it } from "vitest";
import * as sdk from "./index.js";

describe("sdk", () => {
  it("loads", () => {
    expect(sdk).toBeDefined();
  });
});
