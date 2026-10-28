import { describe, expect, it } from "vitest";
import { shortenHome } from "./paths";

describe("shortenHome", () => {
  const home = "/home/dev";

  it("writes the home directory as ~", () => {
    expect(shortenHome("not set in /home/dev/.npmrc", home)).toBe("not set in ~/.npmrc");
    expect(shortenHome("/home/dev", home)).toBe("~");
    expect(shortenHome("JAVA_HOME=/home/dev/.local/share/jdk/21", home)).toBe(
      "JAVA_HOME=~/.local/share/jdk/21",
    );
  });

  it("shortens every occurrence", () => {
    expect(shortenHome("/home/dev/a and /home/dev/b", home)).toBe("~/a and ~/b");
  });

  it("leaves other users and paths that only look alike alone", () => {
    expect(shortenHome("/home/developer/.npmrc", home)).toBe("/home/developer/.npmrc");
    expect(shortenHome("/srv/home/dev/x", home)).toBe("/srv/home/dev/x");
    expect(shortenHome("/home/dev-2/x", home)).toBe("/home/dev-2/x");
    expect(shortenHome("/opt/jdk", home)).toBe("/opt/jdk");
  });

  it("copes with a trailing slash and special characters in the home", () => {
    expect(shortenHome("in /home/dev/x", "/home/dev/")).toBe("in ~/x");
    expect(shortenHome("in /home/d.v/x and /home/dxv/x", "/home/d.v")).toBe(
      "in ~/x and /home/dxv/x",
    );
  });

  it("returns the text unchanged without a home, or for the root directory", () => {
    expect(shortenHome("/home/dev/.npmrc", undefined)).toBe("/home/dev/.npmrc");
    expect(shortenHome("/home/dev/.npmrc", "")).toBe("/home/dev/.npmrc");
    expect(shortenHome("/etc/hosts", "/")).toBe("/etc/hosts");
  });
});
