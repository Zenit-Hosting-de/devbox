import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it } from "vitest";
import { queryAll, setUpApp, settle } from "../testing/test-app";
import { CredentialsPage } from "./credentials";

describe("CredentialsPage (against the mock backend)", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("shows the instances read-only with secret state, expiry, verification and consumers", async () => {
    const fixture = TestBed.createComponent(CredentialsPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "tbody tr")).toHaveLength(2);
    });
    const [git, team] = queryAll(fixture, "tbody tr");
    expect(git?.textContent).toContain("git-example");
    expect(git?.textContent).toContain("git-host");
    expect(git?.textContent).toContain("User");
    expect(git?.textContent).toContain("Set");
    expect(git?.textContent).toMatch(/Expires: \d{1,2}\/\d{1,2}\/\d{2}/);
    expect(git?.textContent).toContain("Valid");
    expect(git?.textContent).toContain("git");

    expect(team?.textContent).toContain("Profile example-team");
    expect(team?.textContent).toContain("Missing");
    expect(team?.textContent).toContain("Failed");
    expect(team?.textContent).toContain("HTTP 401");
    expect(team?.textContent).toContain("maven, npm");
    // Read-only: no inputs and no editing buttons, only the note.
    expect(queryAll(fixture, "input, textarea, select")).toHaveLength(0);
    expect(fixture.nativeElement.textContent).toContain("devbox cred");
  });
});
