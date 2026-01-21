import { test, expect, type Page } from '@playwright/test';
import { fetchContainersWithRetry, type Paginated } from '../utils/fetch.util';
import { ContainerSummary } from 'types/containers.type';

const CONTAINERS_ROUTE = '/containers';

async function navigateToContainers(page: Page) {
  await page.goto(CONTAINERS_ROUTE);
  await page.waitForLoadState('networkidle');
}

let containersData: Paginated<ContainerSummary> = { data: [], pagination: { totalItems: 0 } };

test.beforeEach(async ({ page }) => {
  await navigateToContainers(page);
  containersData = await fetchContainersWithRetry(page);
});

test.describe('Containers Page', () => {
  test('should display the containers page title and description', async ({ page }) => {
    await navigateToContainers(page);
    await expect(page.getByRole('heading', { name: 'Containers', level: 1 })).toBeVisible();
    await expect(page.getByText('View and Manage your Containers').first()).toBeVisible();
  });

  test('should display stat cards with correct counts', async ({ page }) => {
    await navigateToContainers(page);

    const total = containersData.pagination?.totalItems ?? containersData.data.length;
    const running = containersData.data.filter((c) => c.state === 'running').length;
    const stopped = containersData.data.filter((c) => c.state !== 'running').length;

    await expect(page.getByText(`${total} Total`)).toBeVisible();
    await expect(page.getByText(`${running} Running`)).toBeVisible();
    await expect(page.getByText(`${stopped} Stopped`)).toBeVisible();
  });

  test('should display the container table with columns', async ({ page }) => {
    await navigateToContainers(page);
    await expect(page.locator('table')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Name' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'ID' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Image' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'State' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Created' })).toBeVisible();
  });

  test('should navigate to container details on Inspect', async ({ page }) => {
    test.skip(containersData.data.length === 0, 'No containers available');
    await navigateToContainers(page);

    const firstRow = page.locator('tbody tr').first();
    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Inspect', exact: true }).click();

    await expect(page).toHaveURL(/\/containers\/.+/);
    await expect(page.getByRole('heading', { name: 'Container Details', level: 2 }).first()).toBeVisible();
  });

  test('should show correct actions based on container state (without changing state)', async ({ page }) => {
    const running = containersData.data.find((c) => c.state === 'running');
    const stopped = containersData.data.find((c) => c.state !== 'running');

    await navigateToContainers(page);

    if (running) {
      const row = page.locator(`tr:has(a[href="/containers/${running.id}"])`);
      await row.getByRole('button', { name: 'Open menu' }).click();
      await expect(page.getByRole('menuitem', { name: 'Restart', exact: true })).toBeVisible();
      await expect(page.getByRole('menuitem', { name: 'Stop', exact: true })).toBeVisible();
      await page.keyboard.press('Escape');
    } else {
      test.info().annotations.push({ type: 'note', description: 'No running container to validate actions' });
    }

    if (stopped) {
      const row = page.locator(`tr:has(a[href="/containers/${stopped.id}"])`);
      await row.getByRole('button', { name: 'Open menu' }).click();
      await expect(page.getByRole('menuitem', { name: 'Start', exact: true })).toBeVisible();
      await page.keyboard.press('Escape');
    } else {
      test.info().annotations.push({ type: 'note', description: 'No stopped container to validate actions' });
    }
  });

  test('should open the Remove dialog from row actions and allow cancel', async ({ page }) => {
    test.skip(containersData.data.length === 0, 'No containers available');
    const any = containersData.data[0];

    await navigateToContainers(page);

    const row = page.locator(`tr:has(a[href="/containers/${any.id}"])`);
    await row.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Remove', exact: true }).click();

    const dialog = page.locator('div[role="heading"][aria-level="2"][data-dialog-title]:has-text("Confirm Container Removal")');
    await expect(dialog).toBeVisible();

    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });
});
