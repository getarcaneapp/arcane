import { test, expect, type Page } from "@playwright/test";

const ROUTES = {
  environments: "/environments",
};

async function openNewEnvironmentSheet(page: Page) {
  await page.goto(ROUTES.environments);
  await page.waitForLoadState("networkidle");

  const addButton = page.getByRole("button", { name: "Add Environment", exact: true });
  await expect(addButton).toBeVisible();
  await addButton.click();

  await expect(page.getByText("Create New Agent Environment")).toBeVisible();
}

async function switchToEdgeMode(page: Page) {
  await page.getByRole("tab", { name: "Edge", exact: true }).click();
  await expect(page.getByText("Agent connects outbound to the manager.")).toBeVisible();
}

test.describe("Edge Agent Environment", () => {
  test("should display the edge agent form", async ({ page }) => {
    await openNewEnvironmentSheet(page);

    await switchToEdgeMode(page);

    await expect(page.getByPlaceholder("Remote Docker Host")).toBeVisible();
    await expect(page.getByRole("button", { name: "Generate Agent Configuration", exact: true })).toBeVisible();
  });

  test("should switch between direct and edge connection modes", async ({ page }) => {
    await openNewEnvironmentSheet(page);

    await expect(page.locator("#new-agent-api-url")).toBeVisible();
    await expect(page.getByPlaceholder("Remote Docker Host")).toBeHidden();

    await switchToEdgeMode(page);
    await expect(page.locator("#new-agent-api-url")).toBeHidden();
    await expect(page.getByPlaceholder("Remote Docker Host")).toBeVisible();

    await page.getByRole("tab", { name: "Direct", exact: true }).click();
    await expect(page.locator("#new-agent-api-url")).toBeVisible();
    await expect(page.getByPlaceholder("Remote Docker Host")).toBeHidden();
  });

  test("should validate required fields for edge and direct modes", async ({ page }) => {
    await openNewEnvironmentSheet(page);
    let createRequests = 0;

    await page.route("**/api/environments", async (route) => {
      if (route.request().method() === "POST") {
        createRequests += 1;
      }
      await route.continue();
    });

    // Direct mode: missing name and URL
    await page.getByRole("button", { name: "Generate Agent Configuration", exact: true }).click();
    await expect.poll(() => createRequests).toBe(0);

    // Edge mode: missing name
    await switchToEdgeMode(page);
    await page.getByRole("button", { name: "Generate Agent Configuration", exact: true }).click();
    await expect.poll(() => createRequests).toBe(0);
  });

  test("should create an edge agent environment and show deployment snippets", async ({ page }) => {
    const environmentName = `edge-agent-${Date.now().toString().slice(-6)}`;
    let createdEnvironmentId: string | null = null;

    await page.route("**/api/environments", async (route) => {
      if (route.request().method() === "POST") {
        const response = await route.fetch();
        const body = await response.text();
        try {
          const parsed = JSON.parse(body);
          createdEnvironmentId = parsed?.data?.id ?? parsed?.id ?? null;
        } catch {
          createdEnvironmentId = createdEnvironmentId ?? null;
        }

        await route.fulfill({
          status: response.status(),
          headers: response.headers(),
          body,
        });
        return;
      }

      await route.continue();
    });

    try {
      await openNewEnvironmentSheet(page);
      await switchToEdgeMode(page);

      await page.getByPlaceholder("Remote Docker Host").fill(environmentName);
      const submitButton = page.getByRole("button", { name: "Generate Agent Configuration", exact: true });
      await submitButton.click();

      const sheetTitle = page.locator('[data-slot="sheet-title"]');
      await expect(sheetTitle).toContainText(/Environment Created/i);
      await expect(page.getByText("Edge agent - connects outbound to manager", { exact: true })).toBeVisible();
      await expect(page.getByText("API Key", { exact: true })).toBeVisible();
      await expect(page.getByText("Docker Run Command", { exact: true })).toBeVisible();
      await expect(page.getByText("Docker Compose", { exact: true })).toBeVisible();

      const snippetBlocks = page.locator("pre code").filter({ hasText: "EDGE_AGENT=true" });
      await expect(snippetBlocks.first()).toBeVisible();

      const dockerRunSnippet = snippetBlocks.first();
      await expect(dockerRunSnippet).toContainText("arcane-edge-agent");
      await expect(dockerRunSnippet).not.toContainText("-p 3553:3553");

      await page.getByRole("button", { name: "Done", exact: true }).click();

      await expect(page.getByRole("button", { name: environmentName, exact: true })).toBeVisible();
      const environmentRow = page.locator("tr").filter({
        has: page.getByRole("button", { name: environmentName, exact: true }),
      });
      await expect(environmentRow.getByText("edge://edge-agent-").first()).toBeVisible();
      await expect(environmentRow.getByText("gRPC", { exact: true })).toBeVisible();

      await page.getByRole("button", { name: environmentName, exact: true }).click();
      if (createdEnvironmentId) {
        await expect(page).toHaveURL(new RegExp(`/environments/${createdEnvironmentId}`));
      }

      await expect(page.locator("#api-url")).toHaveValue(/edge:\/\/edge-agent-/);
      await expect(page.getByText("gRPC", { exact: true }).first()).toBeVisible();
    } finally {
      if (createdEnvironmentId) {
        await page.request.delete(`/api/environments/${createdEnvironmentId}`);
      }
    }
  });
});
