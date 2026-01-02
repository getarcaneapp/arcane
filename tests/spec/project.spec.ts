import { test, expect, type Page } from '@playwright/test';
import { fetchProjectCountsWithRetry, fetchProjectsWithRetry } from '../utils/fetch.util';
import { Project, ProjectStatusCounts } from 'types/project.type';
import { TEST_COMPOSE_YAML, TEST_ENV_FILE } from '../setup/project.data';

const ROUTES = {
  page: '/projects',
  apiProjects: '/api/environments/0/projects',
  newProject: '/projects/new',
};

async function navigateToProjects(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState('networkidle');
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

test.describe('Projects Page', () => {
  test('should display the main heading and description', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
    await expect(page.getByText('View and Manage Compose Projects')).toBeVisible();
  });

  test('should display summary cards with correct counts', async ({ page }) => {
    await expect(page.getByText(`${projectCounts.totalProjects} Total Projects`, { exact: true })).toBeVisible();
    await expect(page.getByText(`${projectCounts.runningProjects} Running`, { exact: true })).toBeVisible();
    await expect(page.getByText(`${projectCounts.stoppedProjects} Stopped`, { exact: true })).toBeVisible();
  });

  test('should display projects list', async ({ page }) => {
    await expect(page.locator('table')).toBeVisible();
  });

  test('should show project actions menu', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for actions menu test');

    await page.waitForLoadState('networkidle');
    const firstRow = page.locator('tbody tr').first();
    const menuButton = firstRow.getByRole('button', { name: 'Open menu' });
    await expect(menuButton).toBeVisible();
    await menuButton.click();

    await expect(page.getByRole('menuitem', { name: 'Edit' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: /Up|Down|Restart/ })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Pull & Redeploy' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Destroy' })).toBeVisible();
  });

  test('should navigate to project details when project name is clicked', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for navigation test');

    await page.waitForLoadState('networkidle');
    const firstProjectLink = page.locator('tbody tr').first().getByRole('link');
    const projectName = await firstProjectLink.textContent();

    await firstProjectLink.click();
    await expect(page).toHaveURL(/\/projects\/.+/);
    await expect(page.getByRole('heading', { name: new RegExp(`.*${projectName}`) })).toBeVisible();
  });

  test('should allow searching/filtering projects', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for search test');

    const searchInput = page.getByPlaceholder('Search…');
    await expect(searchInput).toBeVisible();

    const firstProject = realProjects[0];
    if (firstProject?.name) {
      await searchInput.fill(firstProject.name);
      await expect(page.getByRole('link', { name: firstProject.name })).toBeVisible();
      await searchInput.clear();
    }
  });

  test('should display project status badges', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for status badge test');

    await page.waitForLoadState('networkidle');

    const runningProjects = realProjects.filter((p) => p.status === 'running');
    const stoppedProjects = realProjects.filter((p) => p.status === 'stopped');

    if (runningProjects.length > 0) {
      await expect(page.locator('text="Running"').first()).toBeVisible();
    }

    if (stoppedProjects.length > 0) {
      await expect(page.locator('text="Stopped"').first()).toBeVisible();
    }
  });
});

test.describe('New Compose Project Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(ROUTES.newProject);
    await page.waitForLoadState('networkidle');
  });

  test('should display the create project form', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'My New Project' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Docker Compose File' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Environment (.env)' })).toBeVisible();
  });

  test('should validate required fields', async ({ page }) => {
    const createButton = page.getByRole('button', { name: 'Create Project' }).locator('[data-slot="arcane-button"]');
    await expect(createButton).toBeDisabled();

    await page.getByRole('button', { name: 'My New Project' }).click();
    await page.getByRole('textbox', { name: 'My New Project' }).fill('test-project');
    await page.getByRole('textbox', { name: 'My New Project' }).press('Enter');
  });

  test('should enable Create Project after entering a valid name', async ({ page }) => {
    const observedErrors: string[] = [];
    page.on('pageerror', (err) => observedErrors.push(String(err?.message ?? err)));
    page.on('console', (msg) => {
      if (msg.type() === 'error') observedErrors.push(msg.text());
    });

    const createButton = page.getByRole('button', { name: 'Create Project' }).locator('[data-slot="arcane-button"]');

    await expect(createButton).toBeVisible();

    // Open the inline name editor and set a valid name.
    await page.getByRole('button', { name: 'My New Project' }).click();
    await page.getByRole('textbox', { name: 'My New Project' }).fill('test-project');
    await page.getByRole('textbox', { name: 'My New Project' }).press('Enter');

    // The button should become enabled once name + compose content are present.
    await expect(createButton).toBeEnabled();

    const stateUnsafe = observedErrors.filter((e) => e.includes('state_unsafe_mutation'));
    expect(stateUnsafe, `Unexpected state_unsafe_mutation errors: ${stateUnsafe.join('\n')}`).toHaveLength(0);
  });

  test('should create a new project successfully', async ({ page }) => {
    const projectName = `test-project-${Date.now()}`;
    let createdProjectId: string | null = null;

    await page.getByRole('button', { name: 'My New Project' }).click();
    await page.getByRole('textbox', { name: 'My New Project' }).fill(projectName);
    await page.getByRole('textbox', { name: 'My New Project' }).press('Enter');

    const composeEditor = page.locator('.monaco-editor').first();
    await expect(composeEditor).toBeVisible();

    // Wait for Monaco to actually render its view before attempting input.
    await expect(composeEditor.locator('.view-line').first()).toBeVisible();

    // Monaco may create the internal input textarea lazily (e.g. only after focus).
    // Click first, then wait for textarea.inputarea to appear.
    await composeEditor.click({ position: { x: 20, y: 20 } });
    await expect(composeEditor.locator('textarea')).toHaveCount(1);

    // Use page.evaluate to set the value directly in Monaco to avoid auto-indentation issues during typing
    await page.evaluate(
      ({ text, lang }) => {
        const models = (window as any).monaco.editor.getModels();
        const model = models.find((m: any) => m.getLanguageId() === lang);
        if (!model) throw new Error(`No ${lang} model found`);
        model.setValue(text);
      },
      { text: TEST_COMPOSE_YAML, lang: 'yaml' },
    );

    // Basic sanity check that the new content rendered.
    await expect(composeEditor.locator('.view-lines')).toContainText(/redis/i);

    const envEditor = page.locator('.monaco-editor').nth(1);
    await expect(envEditor).toBeVisible();

    await expect(envEditor.locator('.view-line').first()).toBeVisible();

    await envEditor.click({ position: { x: 20, y: 20 } });
    await expect(envEditor.locator('textarea')).toHaveCount(1);

    // Use page.evaluate to set the value directly in Monaco
    await page.evaluate(
      ({ text, lang }) => {
        const models = (window as any).monaco.editor.getModels();
        const model = models.find((m: any) => m.getLanguageId() === lang);
        if (!model) throw new Error(`No ${lang} model found`);
        model.setValue(text);
      },
      { text: TEST_ENV_FILE, lang: 'ini' },
    );

    await expect(envEditor.locator('.view-lines')).toContainText(/redis/i);

    await page.route('/api/environments/*/projects', async (route) => {
      if (route.request().method() === 'POST') {
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

    const createButton = page.getByRole('button', { name: 'Create Project' }).locator('[data-slot="arcane-button"]');
    await createButton.click();

    await page.waitForURL(/\/projects\/.+/, { timeout: 10000 });

    if (createdProjectId) {
      await expect(page).toHaveURL(new RegExp(`/projects/${createdProjectId}`));
    } else {
      await expect(page).toHaveURL(new RegExp(`/projects/[a-f0-9\\-]{36}`));
    }

    await expect(page.getByRole('button', { name: projectName })).toBeVisible();

    await page.getByRole('tab', { name: 'Services' }).click();
    await page.waitForLoadState('networkidle');

    const serviceNameWhenStopped = page.getByRole('heading', { name: 'redis', exact: true });
    await expect(serviceNameWhenStopped).toBeVisible();

    const containerNameWhenStopped = page.getByRole('link', { name: 'test-redis-container redis' });
    await expect(containerNameWhenStopped).not.toBeVisible();

    const deployButton = page.getByRole('button', { name: 'Up', exact: true }).filter({ hasText: 'Up' }).last();
    await deployButton.click();

    await page.waitForTimeout(5000);
    await page.waitForLoadState('networkidle');

    const containerNameElement = page.getByRole('link', { name: 'test-redis-container redis' });
    await expect(containerNameElement).toBeVisible({ timeout: 15000 });
  });

  test('should destroy the project and remove files from disk', async ({ page }) => {
    const projectName = `test-destroy-${Date.now()}`;

    // 1. Create the project first
    await page.getByRole('button', { name: 'My New Project' }).click();
    await page.getByRole('textbox', { name: 'My New Project' }).fill(projectName);
    await page.getByRole('textbox', { name: 'My New Project' }).press('Enter');

    const composeEditor = page.locator('.monaco-editor').first();
    await expect(composeEditor).toBeVisible();
    await expect(composeEditor.locator('.view-line').first()).toBeVisible();
    await composeEditor.click({ position: { x: 20, y: 20 } });

    await page.evaluate(
      ({ text, lang }) => {
        const models = (window as any).monaco.editor.getModels();
        const model = models.find((m: any) => m.getLanguageId() === lang);
        if (!model) throw new Error(`No ${lang} model found`);
        model.setValue(text);
      },
      { text: TEST_COMPOSE_YAML, lang: 'yaml' },
    );

    const createButton = page.locator('button[data-slot="arcane-button"]').filter({ hasText: 'Create Project' });
    await createButton.click();

    await page.waitForURL(/\/projects\/.+/, { timeout: 10000 });
    await expect(page.getByRole('button', { name: projectName })).toBeVisible();

    // 2. Destroy the project
    const destroyButton = page.getByRole('button', { name: 'Destroy', exact: true });
    await expect(destroyButton).toBeVisible();
    await destroyButton.click();

    // 3. Handle the confirmation dialog
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Check "Remove project files"
    const removeFilesCheckbox = dialog.getByLabel(/Remove project files/i);
    await removeFilesCheckbox.check();

    // Click "Destroy" in the dialog
    const confirmDestroyButton = dialog.getByRole('button', { name: 'Destroy', exact: true });
    await confirmDestroyButton.click();

    // 4. Verify redirection and project removal
    await page.waitForURL(ROUTES.page, { timeout: 10000 });
    await expect(page.getByRole('link', { name: projectName })).not.toBeVisible();
  });
});

test.describe('Project Detail Page', () => {
  test('should display project details for existing project', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for detail page test');

    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState('networkidle');

    await expect(page.getByRole('button', { name: firstProject.name, exact: false })).toBeVisible();

    await expect(page.getByRole('tab', { name: /Services/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Configuration|Config/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Logs/i })).toBeVisible();
  });

  test('should display tabs navigation', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for navigation test');
    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState('networkidle');

    await expect(page.getByRole('tab', { name: /Services/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Configuration|Config/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Logs/i })).toBeVisible();
  });

  test('should display services tab content', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for services test');

    const projectWithServices = realProjects.find((p) => p.serviceCount > 0) || realProjects[0];
    await page.goto(`/projects/${projectWithServices.id || projectWithServices.name}`);
    await page.waitForLoadState('networkidle');

    await page.getByRole('tab', { name: /Services/i }).click();

    const nginxService = page.getByRole('heading', { name: /nginx/i });
    const emptyState = page.getByText(/No services found/i);

    if ((await nginxService.count()) > 0) {
      await expect(nginxService.first()).toBeVisible();
    } else {
      const anyServiceBadge = page.locator('text=/running|stopped|unknown/i').first();
      if ((await anyServiceBadge.count()) > 0) {
        await expect(anyServiceBadge).toBeVisible();
      } else {
        await expect(emptyState).toBeVisible();
      }
    }
  });

  test('should display configuration editors', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for configuration test');

    const firstProject = realProjects[0];
    await page.goto(`/projects/${firstProject.id || firstProject.name}`);
    await page.waitForLoadState('networkidle');

    const configTab = page.getByRole('tab', { name: /Configuration|Config/i });
    await configTab.click();

    // The project config editor supports two layouts:
    // - classic (default): side-by-side compose.yaml + .env panels
    // - tree view: file list on the left and a single code panel on the right
    await expect(page.getByRole('heading', { name: 'compose.yaml' })).toBeVisible();

    const projectFilesHeading = page.getByRole('heading', { name: /Project Files/i });
    const isTreeView = await projectFilesHeading.isVisible();

    if (isTreeView) {
      const composeFileButton = page.getByRole('button', { name: 'compose.yaml' }).first();
      const envFileButton = page.getByRole('button', { name: '.env' }).first();

      await expect(composeFileButton).toBeVisible();
      await expect(envFileButton).toBeVisible();

      // Switching files should update the visible code panel title
      await envFileButton.click();
      await expect(page.getByRole('heading', { name: '.env' })).toBeVisible();

      await composeFileButton.click();
      await expect(page.getByRole('heading', { name: 'compose.yaml' })).toBeVisible();

      const includesFolder = page.getByRole('button', { name: 'Includes' });
      if (await includesFolder.count()) {
        await expect(includesFolder.first()).toBeVisible();
      }
    } else {
      // Classic layout renders both editors at the same time.
      await expect(page.getByRole('heading', { name: '.env' })).toBeVisible();

      // Also validate that we can switch to tree view and see the file list.
      const layoutSwitch = page.getByRole('switch', { name: /Classic|Tree View/i });
      if (await layoutSwitch.count()) {
        await layoutSwitch.click();
        await expect(projectFilesHeading).toBeVisible();

        const composeFileButton = page.getByRole('button', { name: 'compose.yaml' }).first();
        const envFileButton = page.getByRole('button', { name: '.env' }).first();

        await expect(composeFileButton).toBeVisible();
        await expect(envFileButton).toBeVisible();

        await envFileButton.click();
        await expect(page.getByRole('heading', { name: '.env' })).toBeVisible();
      }
    }
  });

  test('should show logs tab for running projects', async ({ page }) => {
    test.skip(!realProjects.length, 'No projects available for logs test');

    const runningProject = realProjects.find((p) => p.status === 'running');
    test.skip(!runningProject, 'No running projects found for logs test');

    await page.goto(`/projects/${runningProject.id || runningProject.name}`);
    await page.waitForLoadState('networkidle');

    const logsTab = page.getByRole('tab', { name: /Logs/i });
    await expect(logsTab).toBeEnabled();
    await logsTab.click();

    await expect(page.getByRole('heading', { name: 'Project Logs' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Start', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Clear', exact: true })).toBeVisible();
  });
});
