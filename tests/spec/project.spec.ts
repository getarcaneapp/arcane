import { test, expect, type Locator, type Page } from "@playwright/test";
import { fetchProjectCountsWithRetry, fetchProjectsWithRetry } from "../utils/fetch.util";
import { Project, ProjectStatusCounts } from "types/project.type";
import { TEST_COMPOSE_YAML, TEST_ENV_FILE } from "../setup/project.data";

const ROUTES = {
  page: "/projects",
  apiProjects: "/api/environments/0/projects",
  newProject: "/projects/new",
};

async function navigateToProjects(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState("networkidle");
}

async function setCodeMirrorValue(page: Page, editor: Locator, text: string) {
  const content = editor.locator(".cm-content").first();
  await expect(content).toBeVisible();
  await content.click({ position: { x: 10, y: 10 } });
  await content.press("ControlOrMeta+A");
  await page.keyboard.type(text, { delay: 0 });
}

let realProjects: Project[] = [];
let projectCounts: ProjectStatusCounts = { runningProjects: 0, stoppedProjects: 0, totalProjects: 0 };

test.beforeEach(async ({ page }) => {
  await navigateToProjects(page);

  try {
    realProjects = await fetchProjectsWithRetry(page);
    projectCounts = await fetchProjectCountsWithRetry(page);
  } catch (error) {
    realProjects = [];
  }
});

test.describe("Projects Page", () => {
  test("should display the main heading and description", async ({ page }) => {
    await expect(page.getByRole("heading", { name: "Projects", level: 1 })).toBeVisible();
    await expect(page.getByText("View and Manage Compose Projects")).toBeVisible();
  });

  test("should display summary cards with correct counts", async ({ page }) => {
    await expect(page.getByText(`${projectCounts.totalProjects} Total Projects`, { exact: true })).toBeVisible();
    await expect(page.getByText(`${projectCounts.runningProjects} Running`, { exact: true })).toBeVisible();
    await expect(page.getByText(`${projectCounts.stoppedProjects} Stopped`, { exact: true })).toBeVisible();
  });

  test("should display projects list", async ({ page }) => {
    await expect(page.locator("table")).toBeVisible();
  });

  test("should show project actions menu", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for actions menu test");

    await page.waitForLoadState("networkidle");
    const firstRow = page.locator("tbody tr").first();
    const menuButton = firstRow.getByRole("button", { name: "Open menu" });
    await expect(menuButton).toBeVisible();
    await menuButton.click();

    await expect(page.getByRole("menuitem", { name: "Edit" })).toBeVisible();
    // Check for at least one of the state action buttons (Up/Down/Restart)
    const upItem = page.getByRole("menuitem", { name: "Up", exact: true });
    const downItem = page.getByRole("menuitem", { name: "Down", exact: true });
    const restartItem = page.getByRole("menuitem", { name: "Restart", exact: true });
    const hasStateAction = (await upItem.count()) > 0 || (await downItem.count()) > 0 || (await restartItem.count()) > 0;
    expect(hasStateAction).toBe(true);
    await expect(page.getByRole("menuitem", { name: "Pull & Redeploy" })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Destroy" })).toBeVisible();
  });

  test("should navigate to project details when project name is clicked", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for navigation test");

    await page.waitForLoadState("networkidle");
    // Get the first project link that points to /projects/ (not the "Git" indicator link)
    const firstProjectLink = page
      .locator("tbody tr")
      .first()
      .getByRole("link")
      .filter({ hasText: /^(?!Git$)/ })
      .first();
    const projectName = await firstProjectLink.textContent();

    await firstProjectLink.click();
    await expect(page).toHaveURL(/\/projects\/.+/);
    await expect(page.getByRole("button", { name: new RegExp(`${projectName}`) })).toBeVisible();
  });

  test("should allow searching/filtering projects", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for search test");

    const searchInput = page.getByPlaceholder("Search…");
    await expect(searchInput).toBeVisible();

    const firstProject = realProjects[0];
    if (firstProject?.name) {
      await searchInput.fill(firstProject.name);
      await expect(page.getByRole("link", { name: firstProject.name })).toBeVisible();
      await searchInput.clear();
    }
  });

  test("should display project status badges", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for status badge test");

    await page.waitForLoadState("networkidle");

    const runningProjects = realProjects.filter((p) => p.status === "running");
    const stoppedProjects = realProjects.filter((p) => p.status === "stopped");

    if (runningProjects.length > 0) {
      await expect(page.locator('text="Running"').first()).toBeVisible();
    }

    if (stoppedProjects.length > 0) {
      await expect(page.locator('text="Stopped"').first()).toBeVisible();
    }
  });
});

test.describe("New Compose Project Page", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(ROUTES.newProject);
    await page.waitForLoadState("networkidle");
  });

  test("should display the create project form", async ({ page }) => {
    await expect(page.getByRole("button", { name: "My New Project" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Docker Compose File" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Environment (.env)" })).toBeVisible();
  });

  test("should validate required fields", async ({ page }) => {
    const createButton = page.getByRole("button", { name: "Create Project" }).locator('[data-slot="arcane-button"]');
    await expect(createButton).toBeDisabled();

    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill("test-project");
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");
  });

  test("should enable Create Project after entering a valid name", async ({ page }) => {
    const observedErrors: string[] = [];
    page.on("pageerror", (err) => observedErrors.push(String(err?.message ?? err)));
    page.on("console", (msg) => {
      if (msg.type() === "error") observedErrors.push(msg.text());
    });

    const createButton = page.getByRole("button", { name: "Create Project" }).locator('[data-slot="arcane-button"]');

    await expect(createButton).toBeVisible();

    // Open the inline name editor and set a valid name.
    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill("test-project");
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");

    // The button should become enabled once name + compose content are present.
    await expect(createButton).toBeEnabled();

    const stateUnsafe = observedErrors.filter((e) => e.includes("state_unsafe_mutation"));
    expect(stateUnsafe, `Unexpected state_unsafe_mutation errors: ${stateUnsafe.join("\n")}`).toHaveLength(0);
  });

  test("should hide Create Project when compose syntax is invalid and show it when fixed", async ({ page }) => {
    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill("syntax-check-project");
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");

    const composeEditor = page.locator(".cm-editor").first();
    await expect(composeEditor).toBeVisible();

    await setCodeMirrorValue(page, composeEditor, "services:\n\tredis:\n\t\timage: redis:latest\n");
    await expect(page.locator('button[data-slot="arcane-button"]').filter({ hasText: "Create Project" })).toHaveCount(0);

    await setCodeMirrorValue(page, composeEditor, TEST_COMPOSE_YAML);
    const createButton = page.locator('button[data-slot="arcane-button"]').filter({ hasText: "Create Project" });
    await expect(createButton).toBeVisible();
    await expect(createButton).toBeEnabled();
  });

  test("should hide Create Project when env syntax is invalid and show it when fixed", async ({ page }) => {
    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill("env-check-project");
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");

    const composeEditor = page.locator(".cm-editor").first();
    const envEditor = page.locator(".cm-editor").nth(1);
    await expect(composeEditor).toBeVisible();
    await expect(envEditor).toBeVisible();

    await setCodeMirrorValue(page, composeEditor, TEST_COMPOSE_YAML);
    await setCodeMirrorValue(page, envEditor, "NOT VALID LINE");
    await expect(page.locator('button[data-slot="arcane-button"]').filter({ hasText: "Create Project" })).toHaveCount(0);

    await setCodeMirrorValue(page, envEditor, TEST_ENV_FILE);
    const createButton = page.locator('button[data-slot="arcane-button"]').filter({ hasText: "Create Project" });
    await expect(createButton).toBeVisible();
    await expect(createButton).toBeEnabled();
  });

  test("should create a new project successfully", async ({ page }) => {
    const projectName = `test-project-${Date.now()}`;
    const containerName = `test-redis-container-${Date.now()}`;
    const envFile = TEST_ENV_FILE.replace(/CONTAINER_NAME=.*/m, `CONTAINER_NAME=${containerName}`);
    let createdProjectId: string | null = null;
    let projectPullRequestCount = 0;

    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill(projectName);
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");

    const composeEditor = page.locator(".cm-editor").first();
    await expect(composeEditor).toBeVisible();
    await setCodeMirrorValue(page, composeEditor, TEST_COMPOSE_YAML);
    await expect(composeEditor).toContainText(/redis/i);

    const envEditor = page.locator(".cm-editor").nth(1);
    await expect(envEditor).toBeVisible();
    await setCodeMirrorValue(page, envEditor, envFile);
    await expect(envEditor).toContainText(/redis/i);

    await page.route("/api/environments/*/projects", async (route) => {
      if (route.request().method() === "POST") {
        const response = await route.fetch();
        const responseBody = await response.text();

        try {
          const parsed = JSON.parse(responseBody);
          createdProjectId = parsed.id;
        } catch {
          // Keep existing createdProjectId value if parsing fails
        }

        await route.fulfill({
          status: response.status(),
          headers: response.headers(),
          body: responseBody,
        });
      } else {
        await route.continue();
      }
    });

    const createButton = page.getByRole("button", { name: "Create Project" }).locator('[data-slot="arcane-button"]');
    await createButton.click();

    await page.waitForURL(/\/projects\/.+/, { timeout: 10000 });

    if (createdProjectId) {
      await expect(page).toHaveURL(new RegExp(`/projects/${createdProjectId}`));
    } else {
      await expect(page).toHaveURL(new RegExp(`/projects/[a-f0-9\\-]{36}`));
    }

    await expect(page.getByRole("button", { name: projectName })).toBeVisible();

    await page.getByRole("tab", { name: "Services" }).click();
    await page.waitForLoadState("networkidle");

    const serviceTable = page.getByRole("table");
    const serviceNameWhenStopped = serviceTable.getByText("redis", { exact: true });
    const emptyServicesState = page.getByText(/No services found for this project/i);

    if ((await serviceNameWhenStopped.count()) > 0) {
      await expect(serviceNameWhenStopped.first()).toBeVisible();
    } else {
      await expect(emptyServicesState).toBeVisible();
    }

    await page.route("**/api/environments/*/projects/*/pull", async (route) => {
      projectPullRequestCount += 1;
      await route.continue();
    });

    const deployButton = page.getByRole("button", { name: "Up", exact: true }).filter({ hasText: "Up" }).last();
    await deployButton.click();

    await page.waitForTimeout(5000);
    await page.waitForLoadState("networkidle");

    expect(projectPullRequestCount).toBe(0);
    await expect(page.getByText("Running", { exact: true })).toBeVisible({ timeout: 20000 });
    await expect(page.getByRole("button", { name: "Down", exact: true })).toBeVisible();
  });

  test("should destroy the project and remove files from disk", async ({ page }) => {
    const projectName = `test-destroy-${Date.now()}`;

    // 1. Create the project first
    await page.getByRole("button", { name: "My New Project" }).click();
    await page.getByRole("textbox", { name: "My New Project" }).fill(projectName);
    await page.getByRole("textbox", { name: "My New Project" }).press("Enter");

    const composeEditor = page.locator(".cm-editor").first();
    await expect(composeEditor).toBeVisible();
    await setCodeMirrorValue(page, composeEditor, TEST_COMPOSE_YAML);

    const createButton = page.locator('button[data-slot="arcane-button"]').filter({ hasText: "Create Project" });
    await createButton.click();

    await page.waitForURL(/\/projects\/.+/, { timeout: 10000 });
    await expect(page.getByRole("button", { name: projectName })).toBeVisible();

    // 2. Destroy the project
    const destroyButton = page.getByRole("button", { name: "Destroy", exact: true });
    await expect(destroyButton).toBeVisible();
    await destroyButton.click();

    // 3. Handle the confirmation dialog
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    // Check "Remove project files"
    const removeFilesCheckbox = dialog.getByLabel(/Remove project files/i);
    await removeFilesCheckbox.check();

    // Click "Destroy" in the dialog
    const confirmDestroyButton = dialog.getByRole("button", { name: "Destroy", exact: true });
    await confirmDestroyButton.click();

    // 4. Verify redirection and project removal
    await page.waitForURL(ROUTES.page, { timeout: 10000 });
    await expect(page.getByRole("link", { name: projectName })).not.toBeVisible();
  });
});

test.describe("GitOps Managed Project", () => {
  test("should show read-only alert when project is GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy);
    test.skip(!gitOpsProject, "No GitOps-managed projects found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    // Navigate to Configuration tab
    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    // Verify the GitOps read-only alert is visible (title contains "Git" and "Read-only")
    await expect(page.getByText("Git Read-only")).toBeVisible();
    await expect(page.getByText(/managed by Git/i)).toBeVisible();
  });

  test("should display Sync from Git button when GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy);
    test.skip(!gitOpsProject, "No GitOps-managed projects found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    // Verify the Sync from Git button is present
    await expect(page.getByRole("button", { name: "Sync from Git" })).toBeVisible();
  });

  test("should show last sync commit when GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy && p.lastSyncCommit);
    test.skip(!gitOpsProject, "No GitOps-managed projects with sync commit found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    // The commit hash should be visible somewhere on the page
    const commitHash = gitOpsProject!.lastSyncCommit!.substring(0, 7);
    await expect(page.getByText(new RegExp(commitHash))).toBeVisible();
  });

  test("should disable name editing when GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy);
    test.skip(!gitOpsProject, "No GitOps-managed projects found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    // The name button should be disabled for GitOps-managed projects
    const nameButton = page.getByRole("button", { name: gitOpsProject!.name });
    await expect(nameButton).toBeDisabled();
  });

  test("should have compose editor in read-only mode when GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy);
    test.skip(!gitOpsProject, "No GitOps-managed projects found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    await page.waitForTimeout(800);
    const composeContent = page.locator(".cm-editor").first().locator(".cm-content");
    await expect(composeContent).toHaveAttribute("aria-readonly", "true");
  });

  test("should have env editor in read-only mode when GitOps managed", async ({ page }) => {
    const gitOpsProject = realProjects.find((p) => p.gitOpsManagedBy);
    test.skip(!gitOpsProject, "No GitOps-managed projects found");

    await page.goto(`/projects/${gitOpsProject!.id}`);
    await page.waitForLoadState("networkidle");

    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    await page.waitForTimeout(800);
    const envContent = page.locator(".cm-editor").nth(1).locator(".cm-content");
    await expect(envContent).toHaveAttribute("aria-readonly", "true");
  });

  test("should allow editing for non-GitOps managed projects", async ({ page }) => {
    const regularProject = realProjects.find((p) => !p.gitOpsManagedBy && p.status === "stopped");
    test.skip(!regularProject, "No regular (non-GitOps) stopped projects found");

    await page.goto(`/projects/${regularProject!.id}`);
    await page.waitForLoadState("networkidle");

    // The name button should be enabled for regular projects that are stopped
    const nameButton = page.getByRole("button", { name: regularProject!.name });
    await expect(nameButton).toBeEnabled();

    // Navigate to Configuration tab
    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    // GitOps alert should NOT be visible
    await expect(page.getByText("Git Read-only")).not.toBeVisible();

    // Sync from Git button should NOT be visible
    await expect(page.getByRole("button", { name: "Sync from Git" })).not.toBeVisible();
  });

  test("should not show GitOps alert on Configuration tab for regular projects", async ({ page }) => {
    const regularProject = realProjects.find((p) => !p.gitOpsManagedBy);
    test.skip(!regularProject, "No regular (non-GitOps) projects found");

    await page.goto(`/projects/${regularProject!.id}`);
    await page.waitForLoadState("networkidle");

    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();
    await page.waitForLoadState("networkidle");

    // Verify no GitOps-related UI elements
    await expect(page.getByText(/managed by Git\./i)).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Sync from Git" })).not.toBeVisible();
  });
});

test.describe("Project Detail Page", () => {
  test("should display project details for existing project", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for detail page test");

    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState("networkidle");

    await expect(page.getByRole("button", { name: firstProject.name, exact: false })).toBeVisible();

    await expect(page.getByRole("tab", { name: /Services/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /Configuration|Config/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /Logs/i })).toBeVisible();
  });

  test("should display tabs navigation", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for navigation test");
    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState("networkidle");

    await expect(page.getByRole("tab", { name: /Services/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /Configuration|Config/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /Logs/i })).toBeVisible();
  });

  test("should display services tab content", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for services test");

    const projectWithServices = realProjects.find((p) => p.serviceCount > 0) || realProjects[0];
    await page.goto(`/projects/${projectWithServices.id || projectWithServices.name}`);
    await page.waitForLoadState("networkidle");

    await page.getByRole("tab", { name: /Services/i }).click();

    const nginxService = page.getByRole("heading", { name: /nginx/i });
    const emptyState = page.getByText(/No services found/i);

    if ((await nginxService.count()) > 0) {
      await expect(nginxService.first()).toBeVisible();
    } else {
      const anyServiceBadge = page.locator("text=/running|stopped|unknown/i").first();
      if ((await anyServiceBadge.count()) > 0) {
        await expect(anyServiceBadge).toBeVisible();
      } else {
        await expect(emptyState).toBeVisible();
      }
    }
  });

  test("should display configuration editors", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for configuration test");

    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState("networkidle");

    const configTab = page.getByRole("tab", { name: /Configuration|Config/i });
    await configTab.click();

    // The project config editor supports two layouts:
    // - classic (default): side-by-side compose.yaml + .env panels
    // - tree view: file list on the left and a single code panel on the right
    await expect(page.getByRole("heading", { name: "compose.yaml" })).toBeVisible();

    const projectFilesHeading = page.getByRole("heading", { name: /Project Files/i });
    const isTreeView = await projectFilesHeading.isVisible();

    if (isTreeView) {
      const composeFileButton = page.getByRole("button", { name: "compose.yaml" }).first();
      const envFileButton = page.getByRole("button", { name: ".env" }).first();

      await expect(composeFileButton).toBeVisible();
      await expect(envFileButton).toBeVisible();

      // Switching files should update the visible code panel title
      await envFileButton.click();
      await expect(page.getByRole("heading", { name: ".env" })).toBeVisible();

      await composeFileButton.click();
      await expect(page.getByRole("heading", { name: "compose.yaml" })).toBeVisible();

      const includesFolder = page.getByRole("button", { name: "Includes" });
      if (await includesFolder.count()) {
        await expect(includesFolder.first()).toBeVisible();
      }
    } else {
      // Classic layout renders both editors at the same time.
      await expect(page.getByRole("heading", { name: ".env" })).toBeVisible();

      // Also validate that we can switch to tree view and see the file list.
      const layoutSwitch = page.getByRole("switch", { name: /Classic|Tree View/i });
      if (await layoutSwitch.count()) {
        await layoutSwitch.click();
        await expect(projectFilesHeading).toBeVisible();

        const composeFileButton = page.getByRole("button", { name: "compose.yaml" }).first();
        const envFileButton = page.getByRole("button", { name: ".env" }).first();

        await expect(composeFileButton).toBeVisible();
        await expect(envFileButton).toBeVisible();

        await envFileButton.click();
        await expect(page.getByRole("heading", { name: ".env" })).toBeVisible();
      }
    }
  });

  test("should show logs tab for running projects", async ({ page }) => {
    test.skip(!realProjects.length, "No projects available for logs test");

    const runningProject = realProjects.find((p) => p.status === "running");
    test.skip(!runningProject, "No running projects found for logs test");

    await page.goto(`/projects/${runningProject.id || runningProject.name}`);
    await page.waitForLoadState("networkidle");

    const logsTab = page.getByRole("tab", { name: /Logs/i });
    await expect(logsTab).toBeEnabled();
    await logsTab.click();

    const logsSelected = await logsTab.getAttribute("aria-selected");
    if (logsSelected === "true") {
      await expect(page.getByText(/Real-time project logs/i)).toBeVisible();
      await expect(page.getByRole("button", { name: /^(Start|Stop)$/i })).toBeVisible();
      await expect(page.getByRole("button", { name: "Clear", exact: true })).toBeVisible();

      const startButton = page.getByRole("button", { name: "Start", exact: true });
      if ((await startButton.count()) > 0) {
        await startButton.click();
      }

      await expect(page.getByText(/No project selected/i)).not.toBeVisible();
    } else {
      await expect(logsTab).toBeEnabled();
    }
  });
});
