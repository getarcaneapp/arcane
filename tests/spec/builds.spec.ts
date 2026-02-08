import { test, expect, type Locator, type Page } from "@playwright/test";

const ROUTES = {
  page: "/images/builds",
};

const STREAM_SUCCESS =
  '{"type":"build","phase":"begin","service":"manual","status":"build started"}\n' +
  '{"type":"build","phase":"complete","service":"manual","status":"build complete"}\n';

const FIELD_LABELS = {
  cacheTo: "Cache To",
  entitlements: "Entitlements",
  network: "Network",
  isolation: "Isolation",
  shmSize: "Shm Size (bytes)",
  ulimits: "Ulimits",
  extraHosts: "Extra Hosts",
};

async function navigateToBuildWorkspace(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState("networkidle");
  await expect(page.getByRole("heading", { level: 1, name: /Build Workspace/i })).toBeVisible();
}

async function ensureSwitchState(toggle: Locator, desired: boolean) {
  const current = (await toggle.getAttribute("aria-checked")) === "true";
  if (current !== desired) {
    await toggle.click();
  }
}

async function setRequiredBuildInputs(page: Page, tags = `e2e/build:${Date.now()}`) {
  const tagsInput = page.locator("#image-tags");
  await expect(tagsInput).toBeVisible();
  await tagsInput.fill(tags);
}

async function openAdvancedBuildOptions(page: Page) {
  const dockerfileInput = page.locator("#dockerfile");
  const alreadyVisible = await dockerfileInput.isVisible().catch(() => false);
  if (alreadyVisible) {
    return;
  }

  await page.getByRole("button", { name: /Advanced/i }).first().click();
  await expect(dockerfileInput).toBeVisible();
}

async function selectBuildProvider(page: Page, provider: "local" | "depot") {
  const providerTrigger = page.locator('[data-slot="select-trigger"]').first();
  await expect(providerTrigger).toBeVisible();
  await providerTrigger.click();

  const providerLabel = provider === "depot" ? "Depot" : "Local Docker";
  await page.locator('[data-slot="select-item"]').filter({ hasText: providerLabel }).first().click();
}

function getToastTitle(page: Page) {
  return page.locator("li[data-sonner-toast] div[data-title]");
}

function getBuildButton(page: Page) {
  return page.getByRole("button", { name: "Build", exact: true }).first();
}

function upsertSettingEntry(settings: Array<{ key: string; value: unknown }>, key: string, value: unknown) {
  const existing = settings.find((entry) => entry.key === key);
  if (existing) {
    existing.value = value;
    return;
  }
  settings.push({ key, value });
}

function injectDepotSettings(payload: unknown): unknown {
  if (Array.isArray(payload)) {
    const settings = payload.map((entry) => ({ ...(entry as { key: string; value: unknown }) }));
    upsertSettingEntry(settings, "depotProjectId", "e2e-depot-project");
    upsertSettingEntry(settings, "depotToken", "e2e-depot-token");
    upsertSettingEntry(settings, "depotConfigured", "true");
    return settings;
  }

  if (payload && typeof payload === "object") {
    const maybeSettingsObject = payload as Record<string, unknown>;
    if (Array.isArray(maybeSettingsObject.settings)) {
      const settings = maybeSettingsObject.settings.map((entry) => ({ ...(entry as { key: string; value: unknown }) }));
      upsertSettingEntry(settings, "depotProjectId", "e2e-depot-project");
      upsertSettingEntry(settings, "depotToken", "e2e-depot-token");
      upsertSettingEntry(settings, "depotConfigured", "true");
      return { ...maybeSettingsObject, settings };
    }

    return {
      ...maybeSettingsObject,
      depotProjectId: "e2e-depot-project",
      depotToken: "e2e-depot-token",
      depotConfigured: true,
    };
  }

  return {
    depotProjectId: "e2e-depot-project",
    depotToken: "e2e-depot-token",
    depotConfigured: true,
  };
}

async function mockDepotConfiguredSettings(page: Page) {
  await page.route("**/api/environments/0/settings", async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }

    const upstream = await route.fetch();
    const original = await upstream.json().catch(() => ({}));
    const patched = injectDepotSettings(original);

    await route.fulfill({
      status: upstream.status(),
      headers: {
        ...upstream.headers(),
        "content-type": "application/json",
      },
      body: JSON.stringify(patched),
    });
  });
}

test.describe("Build workspace provider flows", () => {
  test("submits local provider build payload from UI controls", async ({ page }) => {
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page);

    const pushSwitch = page.locator("#build-push");
    const loadSwitch = page.locator("#build-load");

    await expect(pushSwitch).toBeVisible();
    await expect(loadSwitch).toBeVisible();

    await ensureSwitchState(pushSwitch, true);
    await ensureSwitchState(loadSwitch, true);

    let buildPayload: Record<string, unknown> | null = null;

    await page.route("**/api/environments/*/images/build", async (route) => {
      buildPayload = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: "application/x-ndjson",
        body: STREAM_SUCCESS,
      });
    });

    await page.getByRole("button", { name: "Build", exact: true }).first().click();

    await expect.poll(() => buildPayload, { timeout: 10000 }).not.toBeNull();

    expect(buildPayload?.provider).toBe("local");
    expect(buildPayload?.push).toBe(true);
    expect(buildPayload?.load).toBe(true);

    await expect(page.getByText("Build completed").first()).toBeVisible();
  });

  test("uses provider selector via UI and enforces depot push/load behavior", async ({ page }) => {
    await mockDepotConfiguredSettings(page);
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page);

    await selectBuildProvider(page, "depot");

    const loadSwitch = page.locator("#build-load");
    await expect(loadSwitch).toBeDisabled();

    let buildPayload: Record<string, unknown> | null = null;

    await page.route("**/api/environments/*/images/build", async (route) => {
      buildPayload = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: "application/x-ndjson",
        body: STREAM_SUCCESS,
      });
    });

    await page.getByRole("button", { name: "Build", exact: true }).first().click();

    await expect.poll(() => buildPayload, { timeout: 10000 }).not.toBeNull();

    expect(buildPayload?.provider).toBe("depot");
    expect(buildPayload?.push).toBe(true);
    expect(buildPayload?.load).toBe(false);
  });

  test("maps advanced local build options from UI into build request payload", async ({ page }) => {
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page, `e2e/local-advanced:${Date.now()}`);
    await openAdvancedBuildOptions(page);

    await page.locator("#dockerfile").fill("Dockerfile.prod");
    await page.locator("#target").fill("builder");
    await page.locator("#platforms").fill("linux/amd64");
    await page.locator("#build-args").fill("FOO=bar\nHTTP_PROXY=http://proxy.local");
    await page.locator("#labels").fill("com.example.team=arcane\ncom.example.env=e2e");
    await page.locator("#cache-from").fill("myorg/myimage:cache,myorg/other:latest");
    await page.getByLabel(FIELD_LABELS.network).fill("host");
    await page.getByLabel(FIELD_LABELS.isolation).fill("process");
    await page.getByLabel(FIELD_LABELS.shmSize).fill("1048576");
    await page.getByLabel(FIELD_LABELS.ulimits).fill("nofile=1024:2048\nnproc=512:1024");
    await page.getByLabel(FIELD_LABELS.extraHosts).fill("registry.local=10.0.0.5\nmirror.local=10.0.0.6");

    await ensureSwitchState(page.locator("#no-cache"), true);
    await ensureSwitchState(page.locator("#pull-base-images"), true);
    await ensureSwitchState(page.locator("#build-push"), false);
    await ensureSwitchState(page.locator("#build-load"), true);

    let buildPayload: Record<string, unknown> | null = null;
    await page.route("**/api/environments/*/images/build", async (route) => {
      buildPayload = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: "application/x-ndjson",
        body: STREAM_SUCCESS,
      });
    });

    await getBuildButton(page).click();
    await expect.poll(() => buildPayload, { timeout: 10000 }).not.toBeNull();

    expect(buildPayload).toMatchObject({
      dockerfile: "Dockerfile.prod",
      target: "builder",
      buildArgs: {
        FOO: "bar",
        HTTP_PROXY: "http://proxy.local",
      },
      labels: {
        "com.example.team": "arcane",
        "com.example.env": "e2e",
      },
      cacheFrom: ["myorg/myimage:cache", "myorg/other:latest"],
      cacheTo: [],
      network: "host",
      isolation: "process",
      shmSize: 1048576,
      ulimits: {
        nofile: "1024:2048",
        nproc: "512:1024",
      },
      entitlements: [],
      privileged: false,
      extraHosts: ["registry.local=10.0.0.5", "mirror.local=10.0.0.6"],
      platforms: ["linux/amd64"],
      noCache: true,
      pull: true,
      provider: "local",
      push: false,
      load: true,
    });
  });

  test("maps depot-supported advanced options and forces push/load in payload", async ({ page }) => {
    await mockDepotConfiguredSettings(page);
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page, `e2e/depot-advanced:${Date.now()}`);
    await openAdvancedBuildOptions(page);
    await selectBuildProvider(page, "depot");

    await expect(page.locator("#build-load")).toBeDisabled();
    await page.getByLabel(FIELD_LABELS.cacheTo).fill("type=registry,ref=myorg/myimage:cache\ntype=inline");
    await page.getByLabel(FIELD_LABELS.entitlements).fill("network.host,security.insecure");
    await page.locator("#platforms").fill("linux/amd64, linux/arm64");
    await ensureSwitchState(page.locator("#privileged-build"), true);
    await ensureSwitchState(page.locator("#build-push"), false);

    let buildPayload: Record<string, unknown> | null = null;
    await page.route("**/api/environments/*/images/build", async (route) => {
      buildPayload = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: "application/x-ndjson",
        body: STREAM_SUCCESS,
      });
    });

    await getBuildButton(page).click();
    await expect.poll(() => buildPayload, { timeout: 10000 }).not.toBeNull();

    expect(buildPayload).toMatchObject({
      cacheTo: ["type=registry", "ref=myorg/myimage:cache", "type=inline"],
      entitlements: ["network.host", "security.insecure"],
      privileged: true,
      platforms: ["linux/amd64", "linux/arm64"],
      provider: "depot",
      push: true,
      load: false,
    });
  });

  test("blocks local provider submission when unsupported local options are configured", async ({ page }) => {
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page, `e2e/local-unsupported:${Date.now()}`);
    await openAdvancedBuildOptions(page);

    await page.getByLabel(FIELD_LABELS.cacheTo).evaluate((el) => {
      (el as HTMLTextAreaElement).disabled = false;
    });
    await page.getByLabel(FIELD_LABELS.entitlements).evaluate((el) => {
      (el as HTMLTextAreaElement).disabled = false;
    });
    await page.locator("#privileged-build").evaluate((el) => {
      const button = el as HTMLButtonElement;
      button.disabled = false;
      button.removeAttribute("disabled");
      button.removeAttribute("data-disabled");
      button.setAttribute("aria-disabled", "false");
    });

    await page.getByLabel(FIELD_LABELS.cacheTo).fill("type=registry,ref=myorg/myimage:cache");
    await page.getByLabel(FIELD_LABELS.entitlements).fill("network.host");
    await page.locator("#platforms").fill("linux/amd64, linux/arm64");
    await ensureSwitchState(page.locator("#privileged-build"), true);

    let buildRequests = 0;
    await page.route("**/api/environments/*/images/build", async (route) => {
      buildRequests += 1;
      await route.abort();
    });

    await getBuildButton(page).click();
    const localUnsupportedToast = getToastTitle(page).filter({
      hasText: /Unsupported build options for provider local:/,
    });
    await expect(localUnsupportedToast.first()).toBeVisible();
    const localUnsupportedText = await localUnsupportedToast.first().innerText();
    expect(localUnsupportedText).toContain("cacheTo");
    expect(localUnsupportedText).toContain("entitlements");
    expect(localUnsupportedText).toContain("platforms");
    await page.waitForTimeout(300);
    expect(buildRequests).toBe(0);
  });

  test("blocks depot provider submission when local-only values exist before switching provider", async ({ page }) => {
    await mockDepotConfiguredSettings(page);
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page, `e2e/depot-unsupported:${Date.now()}`);
    await openAdvancedBuildOptions(page);

    await page.getByLabel(FIELD_LABELS.network).fill("host");
    await page.getByLabel(FIELD_LABELS.isolation).fill("process");
    await page.getByLabel(FIELD_LABELS.shmSize).fill("1024");
    await page.getByLabel(FIELD_LABELS.ulimits).fill("nofile=1024:2048");
    await page.getByLabel(FIELD_LABELS.extraHosts).fill("registry.local=10.0.0.5");

    await selectBuildProvider(page, "depot");

    let buildRequests = 0;
    await page.route("**/api/environments/*/images/build", async (route) => {
      buildRequests += 1;
      await route.abort();
    });

    await getBuildButton(page).click();
    await expect(getToastTitle(page).filter({ hasText: "Unsupported build options for provider depot: extraHosts, isolation, network, shmSize, ulimits" })).toBeVisible();
    await page.waitForTimeout(300);
    expect(buildRequests).toBe(0);
  });

  test("toggles provider-specific advanced field disabled states in the UI", async ({ page }) => {
    await mockDepotConfiguredSettings(page);
    await navigateToBuildWorkspace(page);
    await setRequiredBuildInputs(page, `e2e/provider-ui:${Date.now()}`);
    await openAdvancedBuildOptions(page);

    await expect(page.getByLabel(FIELD_LABELS.cacheTo)).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.entitlements)).toBeDisabled();
    await expect(page.locator("#privileged-build")).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.network)).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.isolation)).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.shmSize)).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.ulimits)).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.extraHosts)).toBeEnabled();

    await selectBuildProvider(page, "depot");

    await expect(page.getByLabel(FIELD_LABELS.cacheTo)).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.entitlements)).toBeEnabled();
    await expect(page.locator("#privileged-build")).toBeEnabled();
    await expect(page.getByLabel(FIELD_LABELS.network)).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.isolation)).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.shmSize)).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.ulimits)).toBeDisabled();
    await expect(page.getByLabel(FIELD_LABELS.extraHosts)).toBeDisabled();
  });
});
