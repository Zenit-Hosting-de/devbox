import { TestBed, type ComponentFixture } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { Repository } from "../api/types";
import { MockBackend, MockProblem } from "../mock/mock-backend";
import { PluginsStore } from "../stores/plugins.store";
import { RepositoriesStore } from "../stores/repositories.store";
import { SessionsStore } from "../stores/sessions.store";
import { byText, queryAll, setUpApp, settle, typeInto } from "../testing/test-app";
import { SourcesPage } from "./sources";

function dialog(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>(".z-dialog");
}

function dialogButton(text: string): HTMLButtonElement | undefined {
  return [...(dialog()?.querySelectorAll("button") ?? [])].find((b) =>
    (b.textContent ?? "").includes(text),
  );
}

function source(fixture: ComponentFixture<unknown>, name: string): HTMLElement {
  const found = (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(
    `[data-source="${name}"]`,
  );
  if (!found) {
    throw new Error(`source ${name} not found`);
  }
  return found;
}

function input(fixture: ComponentFixture<unknown>, id: string): HTMLInputElement {
  const found = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(`#${id}`);
  if (!found) {
    throw new Error(`input ${id} not found`);
  }
  return found;
}

async function open(): Promise<ComponentFixture<SourcesPage>> {
  TestBed.inject(SessionsStore).connect();
  const fixture = TestBed.createComponent(SourcesPage);
  await settle(fixture, () => {
    expect(queryAll(fixture, "z-panel[data-source]").length).toBeGreaterThan(0);
  });
  return fixture;
}

async function openForm(fixture: ComponentFixture<SourcesPage>): Promise<void> {
  byText(fixture, "button", "Add source")?.click();
  await settle(fixture, () => {
    expect(fixture.nativeElement.querySelector("#src-name")).not.toBeNull();
  });
}

function fillForm(
  fixture: ComponentFixture<SourcesPage>,
  values: { name?: string; source?: string; ref?: string; path?: string },
): void {
  const ids = { name: "src-name", source: "src-source", ref: "src-ref", path: "src-path" };
  for (const [key, value] of Object.entries(values)) {
    typeInto(input(fixture, ids[key as keyof typeof ids]), value);
  }
}

describe("SourcesPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("lists the sources with state, address, trust and last sync", async () => {
    const fixture = await open();
    const titles = queryAll(fixture, "z-panel[data-source] .z-panel__title").map((t) =>
      t.textContent?.trim(),
    );
    expect(titles).toEqual(["broken", "corp", "experiments", "team-plugins"]);

    const experiments = source(fixture, "experiments").textContent ?? "";
    expect(experiments).toContain("Waiting for approval");
    expect(experiments).toContain("git+https://example.com/team/devbox-plugins.git");
    expect(experiments).toContain("main");
    expect(experiments).toContain("plugins");
    expect(experiments).toContain("Approval required");
    expect(experiments).toContain("a1b2c3d4e5f6 · 3 plugins");
    expect(experiments).toContain("changes found");
    expect(byText(source(fixture, "experiments"), "button", "Review changes")).toBeDefined();

    const team = source(fixture, "team-plugins").textContent ?? "";
    expect(team).toContain("Pinned commit");
    expect(team).toContain("no changes");
    expect(byText(source(fixture, "team-plugins"), "button", "Review changes")).toBeUndefined();
  });

  it("shows the last error of a source in an alert", async () => {
    const fixture = await open();
    const broken = source(fixture, "broken");
    expect(broken.textContent).toContain("Error");
    expect(broken.querySelector(".z-alert--danger")?.textContent).toContain(
      "git clone failed: repository not found",
    );
    expect(broken.textContent).toContain("Never");
  });

  it("offers no remove button for an entry of the system configuration and says why", async () => {
    const fixture = await open();
    const corp = source(fixture, "corp");
    expect(byText(corp, "button", "Remove")).toBeUndefined();
    expect(byText(corp, "button", "Sync")).toBeDefined();
    expect(corp.textContent).toContain("System");
    expect(corp.textContent).toContain("cannot be removed here");
    expect(byText(source(fixture, "broken"), "button", "Remove")).toBeDefined();
  });

  it("syncs a source", async () => {
    const fixture = await open();
    const sync = vi.spyOn(backend, "syncRepository");
    byText(source(fixture, "team-plugins"), "button", "Sync")?.click();
    await settle(fixture, () => {
      expect(sync).toHaveBeenCalledWith("team-plugins");
      // The mock finds a new revision, which a pinned source activates at once.
      expect(source(fixture, "team-plugins").textContent).toContain("9a8b7c6d5e4f · 1 plugin");
    });
  });

  it("shows the empty state with the explanation and an example", async () => {
    vi.spyOn(backend, "repositoryList").mockReturnValue([]);
    const fixture = TestBed.createComponent(SourcesPage);
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("No sources yet");
    });
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain("Zenit-Hosting-de/devbox-plugins.git");
    expect(byText(fixture, "z-empty-state button", "Add source")).toBeDefined();
  });

  describe("review of a pending revision", () => {
    async function openReview(): Promise<ComponentFixture<SourcesPage>> {
      const fixture = await open();
      byText(source(fixture, "experiments"), "button", "Review changes")?.click();
      await settle(fixture, () => {
        expect(dialog()).not.toBeNull();
      });
      return fixture;
    }

    function plugin(name: string): HTMLElement {
      const found = queryAll(dialog() as HTMLElement, "article").find(
        (a) => a.getAttribute("aria-label") === name,
      );
      if (!found) {
        throw new Error(`plugin ${name} not in the review`);
      }
      return found;
    }

    it("groups the plugins by change and marks new permissions", async () => {
      await openReview();
      const text = dialog()?.textContent ?? "";
      expect(text).toContain("Review source experiments");
      expect(text).toContain("Revision 123456abcdef");
      expect(text).toContain("New plugins (1)");
      expect(text).toContain("Changed plugins (1)");
      expect(text).toContain("Removed plugins (1)");
      expect(text).toContain("1 plugin is unchanged");

      // Added: every permission is new; sudo is warned about and the commands are listed.
      const deploy = plugin("Deploy");
      expect(deploy.textContent).toContain("Network: deploy.example.com");
      expect(deploy.textContent).toContain("Administrator rights (sudo)");
      expect(deploy.textContent).toContain("Needs the commands: rsync, ssh");
      expect(queryAll(deploy, "z-badge").map((b) => b.textContent?.trim())).toEqual([
        "new",
        "new",
        "new",
      ]);
      expect(dialog()?.querySelector(".z-alert--danger")?.textContent).toContain(
        "Administrator rights requested",
      );
      expect(dialog()?.querySelector(".z-alert--danger")?.textContent).toContain("Deploy");

      // Changed: previous to new version, only the permission that is new carries the badge.
      const lint = plugin("Lint");
      expect(lint.textContent).toContain("1.0.0 → 1.1.0");
      const rows = queryAll(lint, "li");
      expect(rows).toHaveLength(2);
      expect(rows[0]?.textContent).not.toContain("new");
      expect(rows[1]?.textContent).toContain("Network: rules.example.com");
      expect(rows[1]?.textContent).toContain("new");

      // Removed: no permissions, just the note.
      expect(plugin("Retired").textContent).toContain("no longer loaded");
      expect(queryAll(plugin("Retired"), "li")).toHaveLength(0);
    });

    it("approves the revision that was shown, loads the plugins and closes", async () => {
      const fixture = await openReview();
      const approve = vi.spyOn(backend, "approveRepository");
      dialogButton("Approve and load")?.click();
      await settle(fixture, () => {
        expect(dialog()).toBeNull();
      });
      expect(approve).toHaveBeenCalledWith("experiments", "123456abcdef");
      const panel = source(fixture, "experiments");
      expect(panel.textContent).toContain("Active");
      expect(panel.textContent).toContain("123456abcdef · 3 plugins");
      expect(byText(panel, "button", "Review changes")).toBeUndefined();
      await vi.waitFor(() => {
        expect(TestBed.inject(PluginsStore).plugin("deploy")?.origin.repository).toBe(
          "experiments",
        );
      });
    });

    it("discards the revision", async () => {
      const fixture = await openReview();
      const discard = vi.spyOn(backend, "discardRepository");
      dialogButton("Discard")?.click();
      await settle(fixture, () => {
        expect(dialog()).toBeNull();
      });
      expect(discard).toHaveBeenCalledWith("experiments");
      expect(source(fixture, "experiments").textContent).toContain("a1b2c3d4e5f6");
      expect(byText(source(fixture, "experiments"), "button", "Review changes")).toBeUndefined();
    });

    it("blocks approving when another revision is pending now", async () => {
      const fixture = await openReview();
      const approve = vi.spyOn(backend, "approveRepository");
      TestBed.inject(RepositoriesStore).repositories.update((list) =>
        list.map((r) =>
          r.name === "experiments" && r.pending
            ? { ...r, pending: { ...r.pending, revision: "ffffffffffff" } }
            : r,
        ),
      );
      await settle(fixture, () => {
        expect(dialog()?.textContent).toContain("A different revision is waiting now");
      });
      expect(dialogButton("Approve and load")?.disabled).toBe(true);
      dialogButton("Approve and load")?.click();
      await settle(fixture);
      expect(approve).not.toHaveBeenCalled();
    });

    it("shows the problem when the core refuses", async () => {
      const fixture = await openReview();
      vi.spyOn(backend, "approveRepository").mockImplementation(() => {
        throw new MockProblem(409, "conflict", "The pending revision is another one.");
      });
      dialogButton("Approve and load")?.click();
      await settle(fixture, () => {
        expect(dialog()?.textContent).toContain("The pending revision is another one.");
      });
      expect(dialog()).not.toBeNull();
    });
  });

  describe("add source", () => {
    it("checks the input before anything is sent", async () => {
      const fixture = await open();
      const create = vi.spyOn(backend, "createRepository");
      await openForm(fixture);
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("A value is required.");
      });
      fillForm(fixture, { name: "Bad Name", source: "ftp://example.com/x" });
      await settle(fixture, () => {
        const text = fixture.nativeElement.textContent as string;
        expect(text).toContain("The name needs 2 to 32 lowercase letters");
        expect(text).toContain("The address must start with git+https://");
      });
      expect(create).not.toHaveBeenCalled();
    });

    it("explains the address formats and the trust choices", async () => {
      const fixture = await open();
      await openForm(fixture);
      const text = fixture.nativeElement.textContent as string;
      for (const scheme of ["git+https://", "git+ssh://", "https://", "file://", "git+file://"]) {
        expect(text).toContain(scheme);
      }
      expect(text).toContain("Ask for approval");
      expect(text).toContain("loaded without a review");
      // Pinned needs a full commit.
      fillForm(fixture, { name: "pinned", source: "git+https://example.com/a.git", ref: "main" });
      byText(fixture, "label.z-option", "Pinned commit")?.querySelector("input")?.click();
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("needs a Git source and a full");
      });
    });

    it("adds the source, closes the form and lists the new entry for review", async () => {
      const fixture = await open();
      const create = vi.spyOn(backend, "createRepository");
      await openForm(fixture);
      fillForm(fixture, {
        name: "demo-src",
        source: " file:///tmp/demo ",
        path: "plugins",
      });
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(source(fixture, "demo-src")).toBeDefined();
      });
      expect(create).toHaveBeenCalledWith({
        name: "demo-src",
        source: "file:///tmp/demo",
        path: "plugins",
        trust: "prompt",
      });
      expect(fixture.nativeElement.querySelector("#src-name")).toBeNull();
      expect(source(fixture, "demo-src").textContent).toContain("Waiting for approval");
      expect(byText(source(fixture, "demo-src"), "button", "Review changes")).toBeDefined();
    });

    it("lists a source whose first sync failed with its error", async () => {
      const fixture = await open();
      await openForm(fixture);
      fillForm(fixture, { name: "failing", source: "https://example.com/fail.tar.gz" });
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(source(fixture, "failing").querySelector(".z-alert--danger")).not.toBeNull();
      });
    });

    it("shows a busy state while the sync runs", async () => {
      const fixture = await open();
      await openForm(fixture);
      fillForm(fixture, { name: "slow", source: "file:///tmp/slow" });
      // Hold the call open, as a long sync does.
      let finish: (entry: Repository) => void = () => undefined;
      vi.spyOn(TestBed.inject(ApiClient), "createRepository").mockReturnValue(
        new Promise<Repository>((resolve) => {
          finish = resolve;
        }),
      );
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("The source is being fetched.");
      });
      expect(input(fixture, "src-name").disabled).toBe(true);
      expect(byText(fixture, "button", "Add and sync")?.getAttribute("aria-busy")).toBe("true");
      finish({
        name: "slow",
        kind: "plugins",
        source: "file:///tmp/slow",
        trust: "prompt",
        enabled: true,
        origin: "user",
        locked: false,
        state: "new",
      });
      await settle(fixture, () => {
        expect(source(fixture, "slow")).toBeDefined();
      });
      expect(fixture.nativeElement.querySelector("#src-name")).toBeNull();
    });

    it("shows a refusal of the core at the field it names", async () => {
      const fixture = await open();
      await openForm(fixture);
      fillForm(fixture, { name: "demo-src", source: "file:///tmp/demo" });
      vi.spyOn(backend, "createRepository").mockImplementation(() => {
        throw new MockProblem(422, "validation", "invalid", [
          { pointer: "/source", code: "pattern", message: "bad address" },
        ]);
      });
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain(
          "The value does not have the expected format.",
        );
      });
      expect(fixture.nativeElement.querySelector("#src-name")).not.toBeNull();
    });

    it("shows a name conflict as a general alert and keeps the form", async () => {
      const fixture = await open();
      await openForm(fixture);
      fillForm(fixture, { name: "corp", source: "file:///tmp/demo" });
      byText(fixture, "button", "Add and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("A repository named corp exists.");
      });
      expect(input(fixture, "src-name").value).toBe("corp");
    });
  });

  describe("a source without plugins in its folder", () => {
    async function addMisplaced(fixture: ComponentFixture<SourcesPage>): Promise<void> {
      await openForm(fixture);
      fillForm(fixture, { name: "wrong-path", source: "git+https://example.com/subfolder.git" });
      fixture.nativeElement.querySelector("form")?.dispatchEvent(new Event("submit"));
      await settle(fixture, () => {
        expect(source(fixture, "wrong-path")).toBeDefined();
      });
    }

    it("warns and offers the folders that hold plugins", async () => {
      const fixture = await open();
      await addMisplaced(fixture);
      const panel = source(fixture, "wrong-path");
      expect(panel.textContent).toContain("Waiting for approval");
      const alert = panel.querySelector(".z-alert--warning");
      expect(alert?.textContent).toContain("No plugins found in this folder");
      expect(queryAll(alert as HTMLElement, "button").map((b) => b.textContent?.trim())).toEqual([
        "Use path plugins",
      ]);
      // sources with plugins do not warn
      expect(source(fixture, "experiments").querySelector(".z-alert--warning")).toBeNull();
      expect(source(fixture, "team-plugins").querySelector(".z-alert--warning")).toBeNull();
    });

    it("sets the folder, syncs and lists the plugins for review", async () => {
      const fixture = await open();
      await addMisplaced(fixture);
      const patch = vi.spyOn(backend, "patchRepository");
      byText(source(fixture, "wrong-path"), "button", "Use path plugins")?.click();
      await settle(fixture, () => {
        expect(patch).toHaveBeenCalledWith("wrong-path", { path: "plugins" });
        expect(source(fixture, "wrong-path").querySelector(".z-alert--warning")).toBeNull();
      });
      const panel = source(fixture, "wrong-path");
      expect(panel.textContent).toContain("plugins");
      expect(panel.textContent).toContain("Waiting for approval");
      expect(byText(panel, "button", "Review changes")).toBeDefined();
    });

    it("warns without buttons when no folder is known", async () => {
      const fixture = await open();
      vi.spyOn(backend, "repositoryList").mockReturnValue([
        {
          name: "bare",
          kind: "plugins",
          source: "file:///tmp/bare",
          trust: "prompt",
          enabled: true,
          origin: "user",
          locked: false,
          state: "active",
          active: { revision: "aabbccddeeff", syncedAt: "2026-03-01T09:00:00Z", plugins: [] },
        },
      ]);
      await TestBed.inject(RepositoriesStore).reload();
      await settle(fixture, () => {
        expect(source(fixture, "bare").querySelector(".z-alert--warning")).not.toBeNull();
      });
      const alert = source(fixture, "bare").querySelector(".z-alert--warning") as HTMLElement;
      expect(alert.textContent).toContain("No plugins found in this folder");
      expect(queryAll(alert, "button")).toHaveLength(0);
    });
  });

  describe("edit source", () => {
    function edit(fixture: ComponentFixture<SourcesPage>, name: string): void {
      byText(source(fixture, name), "button", "Edit")?.click();
    }

    it("offers Edit for sources of the user configuration only", async () => {
      const fixture = await open();
      expect(byText(source(fixture, "experiments"), "button", "Edit")).toBeDefined();
      expect(byText(source(fixture, "team-plugins"), "button", "Edit")).toBeDefined();
      expect(byText(source(fixture, "corp"), "button", "Edit")).toBeUndefined();
    });

    it("opens the form with the values; the name cannot be changed", async () => {
      const fixture = await open();
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-name").value).toBe("experiments");
      });
      expect(input(fixture, "src-name").readOnly).toBe(true);
      expect(input(fixture, "src-source").value).toBe(
        "git+https://example.com/team/devbox-plugins.git",
      );
      expect(input(fixture, "src-ref").value).toBe("main");
      expect(input(fixture, "src-path").value).toBe("plugins");
      const text = fixture.nativeElement.textContent as string;
      expect(text).toContain("Edit source experiments");
      expect(byText(fixture, "button", "Save and sync")).toBeDefined();
      expect(byText(fixture, "button", "Add and sync")).toBeUndefined();
    });

    it("sends the changed fields, null for the ones that were emptied", async () => {
      const fixture = await open();
      const patch = vi.spyOn(backend, "patchRepository");
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-ref").value).toBe("main");
      });
      fillForm(fixture, { ref: "", path: "tools" });
      byText(fixture, "button", "Save and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.querySelector("#src-name")).toBeNull();
      });
      expect(patch).toHaveBeenCalledWith("experiments", {
        source: "git+https://example.com/team/devbox-plugins.git",
        ref: null,
        path: "tools",
      });
      const panel = source(fixture, "experiments");
      expect(panel.textContent).toContain("tools");
      expect(panel.textContent).not.toContain("main");
    });

    it("sends the trust when it was changed", async () => {
      const fixture = await open();
      const patch = vi.spyOn(backend, "patchRepository");
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-ref").value).toBe("main");
      });
      fillForm(fixture, { ref: "a".repeat(40) });
      byText(fixture, "label.z-option", "Pinned commit")?.querySelector("input")?.click();
      byText(fixture, "button", "Save and sync")?.click();
      await settle(fixture, () => {
        expect(patch).toHaveBeenCalledWith(
          "experiments",
          expect.objectContaining({ ref: "a".repeat(40), trust: "pinned" }),
        );
      });
    });

    it("checks the input like the add form", async () => {
      const fixture = await open();
      const patch = vi.spyOn(backend, "patchRepository");
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-ref").value).toBe("main");
      });
      fillForm(fixture, { source: "ftp://example.com/x" });
      byText(fixture, "button", "Save and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("The address must start with");
      });
      expect(patch).not.toHaveBeenCalled();
    });

    it("shows a refusal of the core and keeps the form", async () => {
      const fixture = await open();
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-ref").value).toBe("main");
      });
      vi.spyOn(backend, "patchRepository").mockImplementation(() => {
        throw new MockProblem(409, "conflict", "The repository comes from system.");
      });
      byText(fixture, "button", "Save and sync")?.click();
      await settle(fixture, () => {
        expect(fixture.nativeElement.textContent).toContain("The repository comes from system.");
      });
      expect(input(fixture, "src-name").value).toBe("experiments");
    });

    it("switches back to adding when Add source is pressed during an edit", async () => {
      const fixture = await open();
      edit(fixture, "experiments");
      await settle(fixture, () => {
        expect(input(fixture, "src-name").readOnly).toBe(true);
      });
      byText(fixture, "button", "Add source")?.click();
      await settle(fixture, () => {
        expect(input(fixture, "src-name").readOnly).toBe(false);
      });
      expect(input(fixture, "src-name").value).toBe("");
      expect(byText(fixture, "button", "Add and sync")).toBeDefined();
    });
  });

  describe("help in the form", () => {
    it("explains revision and subfolder", async () => {
      const fixture = await open();
      await openForm(fixture);
      const text = fixture.nativeElement.textContent as string;
      expect(text).toContain("Branch, tag or commit (optional)");
      expect(text).toContain("Empty uses the default branch");
      expect(text).toContain("Subfolder with the plugin folders (optional)");
      expect(text).toContain("whose subfolders are the plugins");
      expect(input(fixture, "src-path").placeholder).toBe("plugins");
    });
  });

  describe("remove", () => {
    it("asks first and removes the source", async () => {
      const fixture = await open();
      const remove = vi.spyOn(backend, "deleteRepository");
      byText(source(fixture, "team-plugins"), "button", "Remove")?.click();
      await settle(fixture, () => {
        expect(dialog()).not.toBeNull();
      });
      expect(dialog()?.textContent).toContain("Remove source team-plugins?");
      dialogButton("Cancel")?.click();
      await settle(fixture, () => {
        expect(dialog()).toBeNull();
      });
      expect(remove).not.toHaveBeenCalled();

      byText(source(fixture, "team-plugins"), "button", "Remove")?.click();
      await settle(fixture, () => {
        expect(dialog()).not.toBeNull();
      });
      dialogButton("Remove")?.click();
      await settle(fixture, () => {
        expect(remove).toHaveBeenCalledWith("team-plugins");
        expect(fixture.nativeElement.querySelector('[data-source="team-plugins"]')).toBeNull();
      });
    });
  });
});
