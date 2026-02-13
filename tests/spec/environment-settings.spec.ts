import { test, expect, type Page } from "@playwright/test";

const LOCAL_ENV_ID = "0";

async function openEnvironment(page: Page, environmentId: string) {
  await page.goto(`/environments/${environmentId}`);
  await page.waitForLoadState("networkidle");
  await expect(page.locator("#env-name")).toBeVisible();
  await expect(page.getByRole("button", { name: "Save", exact: true }).first()).toBeVisible();
}

async function createDirectEnvironmentViaUI(page: Page, environmentName: string) {
  await page.goto("/environments");
  await page.waitForLoadState("networkidle");

  await page.getByRole("button", { name: "Add Environment", exact: true }).click();
  await expect(page.getByText("Create New Agent Environment")).toBeVisible();

  await page.locator("input#name:visible").first().fill(environmentName);
  await page.locator("#new-agent-api-url").fill("localhost:3552");
  await page.getByRole("button", { name: "Generate Agent Configuration", exact: true }).click();

  await expect(page.locator('[data-slot="sheet-title"]')).toContainText(/Environment Created/i);
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.getByRole("button", { name: environmentName, exact: true })).toBeVisible();
}

async function deleteEnvironmentViaUI(page: Page, environmentName: string) {
  await page.goto("/environments");
  await page.waitForLoadState("networkidle");

  const envRow = page.locator("tr").filter({
    has: page.getByRole("button", { name: environmentName, exact: true }),
  });

  if ((await envRow.count()) === 0) {
    return;
  }

  await envRow.getByRole("button", { name: /open menu/i }).click();
  await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
  await page.getByRole("button", { name: "Remove", exact: true }).click();
  await expect(page.getByRole("button", { name: environmentName, exact: true })).toHaveCount(0);
}

async function openLocalEnvironment(page: Page) {
  await openEnvironment(page, LOCAL_ENV_ID);
}

async function saveAndWaitForPut(page: Page, expectedPath: string) {
  const saveButton = page.getByRole("button", { name: "Save", exact: true }).first();
  await expect(saveButton).toBeEnabled();

  const requestPromise = page.waitForRequest((request) => {
    if (request.method() !== "PUT") return false;
    const url = new URL(request.url());
    return url.pathname === expectedPath;
  });

  await saveButton.click();
  await requestPromise;
  await expect(saveButton).toBeDisabled();
}

test.describe("Environment Settings UI", () => {
  test("should update and save environment details", async ({ page }) => {
    const envName = `settings-ui-${Date.now().toString().slice(-5)}`;
    const updatedName = `${envName}-updated`;

    try {
      await createDirectEnvironmentViaUI(page, envName);
      await page.getByRole("button", { name: envName, exact: true }).click();
      await expect(page).toHaveURL(/\/environments\/[^/]+$/);

      const environmentId = new URL(page.url()).pathname.split("/").pop()!;
      const nameInput = page.locator("#env-name");
      await nameInput.fill(updatedName);
      await saveAndWaitForPut(page, `/api/environments/${environmentId}`);

      await page.reload();
      await expect(page.locator("#env-name")).toHaveValue(updatedName);
    } finally {
      await deleteEnvironmentViaUI(page, updatedName);
      await deleteEnvironmentViaUI(page, envName);
    }
  });

  test("should update and save general environment settings", async ({ page }) => {
    await openLocalEnvironment(page);

    await page.getByRole("tab", { name: "General", exact: true }).click();
    const projectsDirectoryInput = page.locator("#projects-directory");
    await expect(projectsDirectoryInput).toBeVisible();

    const originalProjectsDirectory = await projectsDirectoryInput.inputValue();
    const updatedProjectsDirectory =
      originalProjectsDirectory.endsWith("-e2e") ? `${originalProjectsDirectory}-x` : `${originalProjectsDirectory}-e2e`;

    try {
      await projectsDirectoryInput.fill(updatedProjectsDirectory);
      await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);

      await page.reload();
      await page.getByRole("tab", { name: "General", exact: true }).click();
      await expect(page.locator("#projects-directory")).toHaveValue(updatedProjectsDirectory);
    } finally {
      await page.getByRole("tab", { name: "General", exact: true }).click();
      const currentProjectsDirectory = await page.locator("#projects-directory").inputValue();
      if (currentProjectsDirectory !== originalProjectsDirectory) {
        await page.locator("#projects-directory").fill(originalProjectsDirectory);
        await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
      }
    }
  });

  test("should reset unsaved environment detail changes", async ({ page }) => {
    await openLocalEnvironment(page);

    const nameInput = page.locator("#env-name");
    const originalName = await nameInput.inputValue();
    await nameInput.fill(`${originalName}-pending`);

    const saveButton = page.getByRole("button", { name: "Save", exact: true }).first();
    const resetButton = page.getByRole("button", { name: "Reset", exact: true }).first();

    await expect(saveButton).toBeEnabled();
    await expect(resetButton).toBeVisible();
    await resetButton.click();

    await expect(nameInput).toHaveValue(originalName);
    await expect(saveButton).toBeDisabled();
  });
});
