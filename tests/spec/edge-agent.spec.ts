import { test, expect, type Page } from "@playwright/test";

const ROUTES = {
  environments: "/environments",
};

function slugifyEdgeName(name: string) {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)+/g, "");
}

async function openNewEnvironmentSheet(page: Page) {
  await page.goto(ROUTES.environments);
  await page.waitForLoadState("networkidle");

  const addButton = page.getByRole("button", { name: "Add Environment", exact: true });
  await expect(addButton).toBeVisible();
  await addButton.click();

  await expect(page.getByText("Create New Agent Environment")).toBeVisible();
}

test.describe("Edge Agent Environment", () => {
  test("should display the edge agent form", async ({ page }) => {
    await openNewEnvironmentSheet(page);

    await page.getByRole("tab", { name: "Edge", exact: true }).click();

    await expect(page.getByText("Agent connects outbound to the manager.")).toBeVisible();
    await expect(page.getByPlaceholder("Remote Docker Host")).toBeVisible();
    await expect(page.getByRole("button", { name: "Generate Agent Configuration", exact: true })).toBeVisible();
  });

  test("should create an edge agent environment and show deployment snippets", async ({ page }) => {
    const environmentName = `edge-agent-${Date.now().toString().slice(-6)}`;
    const expectedApiUrl = `edge://${slugifyEdgeName(environmentName)}`;
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
      await page.getByRole("tab", { name: "Edge", exact: true }).click();

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
    } finally {
      if (createdEnvironmentId) {
        await page.request.delete(`/api/environments/${createdEnvironmentId}`);
      }
    }
  });
});
