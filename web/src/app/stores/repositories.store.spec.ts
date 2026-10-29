import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "../api/problem";
import { MockBackend } from "../mock/mock-backend";
import { setUpApp } from "../testing/test-app";
import { PluginsStore } from "./plugins.store";
import { RepositoriesStore } from "./repositories.store";
import { SessionsStore } from "./sessions.store";

async function rejection(promise: Promise<unknown>): Promise<ApiProblem> {
  try {
    await promise;
  } catch (error) {
    if (error instanceof ApiProblem) {
      return error;
    }
    throw error;
  }
  throw new Error("expected a rejection");
}

describe("RepositoriesStore (against the mock backend)", () => {
  let backend: MockBackend;
  let store: RepositoriesStore;

  beforeEach(() => {
    backend = setUpApp();
    store = TestBed.inject(RepositoriesStore);
  });

  it("loads the sources sorted by name and counts the ones waiting for approval", async () => {
    expect(store.state()).toBe("idle");
    await store.ensureLoaded();
    expect(store.state()).toBe("ready");
    expect(store.repositories().map((r) => r.name)).toEqual([
      "broken",
      "corp",
      "experiments",
      "team-plugins",
    ]);
    expect(store.pendingCount()).toBe(1);
    expect(store.repository("corp")?.origin).toBe("system");
  });

  it("keeps the old list and reports the problem when a reload fails", async () => {
    await store.ensureLoaded();
    vi.spyOn(backend, "repositoryList").mockImplementation(() => {
      throw new Error("down");
    });
    await store.reload();
    expect(store.error()).toBeInstanceOf(ApiProblem);
    expect(store.state()).toBe("ready");
    expect(store.repositories()).toHaveLength(4);
  });

  it("adds a source: busy while the call runs, the new entry waits for approval", async () => {
    await store.ensureLoaded();
    const pending = store.add({ name: "demo-src", source: "file:///tmp/demo", trust: "prompt" });
    expect(store.adding()).toBe(true);
    const created = await pending;
    expect(store.adding()).toBe(false);
    expect(created.state).toBe("pending");
    expect(created.pending?.plugins.map((p) => [p.id, p.change])).toEqual([["demo", "added"]]);
    expect(store.repository("demo-src")).toBeDefined();
  });

  it("returns an entry with state error when the first sync fails", async () => {
    await store.ensureLoaded();
    const created = await store.add({
      name: "failing",
      source: "https://example.com/fail.tar.gz",
      trust: "prompt",
    });
    expect(created.state).toBe("error");
    expect(created.lastError).toContain("failed");
  });

  it("rejects with the problem when the entry is invalid or exists", async () => {
    await store.ensureLoaded();
    const invalid = await rejection(store.add({ name: "X", source: "nope", trust: "prompt" }));
    expect(invalid.status).toBe(422);
    expect(invalid.errorsByPointer().has("/name")).toBe(true);
    expect(invalid.errorsByPointer().has("/source")).toBe(true);
    const duplicate = await rejection(
      store.add({ name: "corp", source: "file:///tmp/x", trust: "prompt" }),
    );
    expect(duplicate.status).toBe(409);
    expect(store.adding()).toBe(false);
  });

  it("approves only the pending revision and then loads the plugins", async () => {
    await store.ensureLoaded();
    const plugins = TestBed.inject(PluginsStore);
    await plugins.ensureLoaded();
    await store.add({ name: "demo-src", source: "file:///tmp/demo", trust: "prompt" });

    const wrong = await rejection(store.approve("demo-src", "000000000000"));
    expect(wrong.status).toBe(409);
    expect(plugins.plugin("demo")).toBeUndefined();

    const revision = store.repository("demo-src")?.pending?.revision ?? "";
    const approved = await store.approve("demo-src", revision);
    expect(approved.state).toBe("active");
    expect(approved.pending).toBeUndefined();
    await vi.waitFor(() => {
      expect(plugins.plugin("demo")?.origin).toEqual({
        kind: "repository",
        repository: "demo-src",
        revision,
      });
    });
  });

  it("discards a pending revision", async () => {
    await store.ensureLoaded();
    const result = await store.discard("experiments");
    expect(result.state).toBe("active");
    expect(store.pendingCount()).toBe(0);
  });

  it("removes a user entry and refuses entries of the system configuration", async () => {
    await store.ensureLoaded();
    await store.remove("team-plugins");
    expect(store.repository("team-plugins")).toBeUndefined();
    const refused = await rejection(store.remove("corp"));
    expect(refused.status).toBe(409);
    expect(store.repository("corp")).toBeDefined();
    expect(store.busy().size).toBe(0);
  });

  it("reloads on the plugins message of the socket", async () => {
    TestBed.inject(SessionsStore).connect();
    await store.ensureLoaded();
    expect(store.repository("experiments")?.state).toBe("pending");
    // Another client (the CLI) discards the revision; the core announces it.
    backend.discardRepository("experiments");
    await vi.waitFor(() => {
      expect(store.repository("experiments")?.state).toBe("active");
    });
  });
});
