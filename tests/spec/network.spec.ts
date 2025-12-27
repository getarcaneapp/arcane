import { test, expect, type Page } from '@playwright/test';
import { fetchNetworksCountsWithRetry, fetchNetworksWithRetry } from '../utils/fetch.util';
import { NetworkSummary, NetworkUsageCounts } from 'types/networks.type';

async function navigateToNetworks(page: Page) {
  await page.goto('/networks');
  await page.waitForLoadState('networkidle');
}

let realNetworks: NetworkSummary[] = [];
let networkCount: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

test.beforeEach(async ({ page }) => {
  await navigateToNetworks(page);
  realNetworks = await fetchNetworksWithRetry(page).catch(() => []);
  networkCount = await fetchNetworksCountsWithRetry(page);
});

test.describe('Networks Page', () => {
  test('Page renders with heading and subtitle', async ({ page }) => {
    await navigateToNetworks(page);
    await expect(page.getByRole('heading', { level: 1, name: 'Networks' })).toBeVisible();
    await expect(page.getByText('Manage your Docker networks').first()).toBeVisible();
  });

  test('Stat cards show correct counts', async ({ page }) => {
    await navigateToNetworks(page);

    // Fetch counts directly in the test to ensure we have fresh data
    const counts = await fetchNetworksCountsWithRetry(page);

    await expect(page.getByText(`${counts.total} Total Networks`)).toBeVisible();
    await expect(page.getByText(`${counts.unused} Unused Networks`)).toBeVisible();
  });

  test('Table displays when networks exist, else empty state', async ({ page }) => {
    await navigateToNetworks(page);
    if (realNetworks.length > 0) {
      await expect(page.locator('table')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Name' })).toBeVisible();
    } else {
      await expect(page.getByText('No networks found')).toBeVisible();
    }
  });

  test('Open Create Network sheet', async ({ page }) => {
    await navigateToNetworks(page);
    await page.getByRole('button', { name: 'Create Network' }).first().click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Create New Network' })).toBeVisible();

    const networkName = `test-network-${Date.now()}`;
    const nameInput = page.getByLabel('Name').first();
    if (await nameInput.isVisible().catch(() => false)) {
      await nameInput.fill(networkName);
    } else {
      await page.locator('input[id^="network-name-"]').first().fill(networkName);
    }

    await page.getByRole('dialog').getByRole('button', { name: 'Create Network' }).click();
    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
  });

  test('Inspect Network from row actions', async ({ page }) => {
    await navigateToNetworks(page);
    const firstRow = page.locator('tbody tr').first();
    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Inspect' }).click();
    await expect(page).toHaveURL(/\/networks\/.+/);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  });

  test('Remove Network from table', async ({ page }) => {
    const networkName = `test-remove-network-${Date.now()}`;
    await navigateToNetworks(page);

    // 1. Create the network first
    await page.getByRole('button', { name: 'Create Network' }).first().click();
    const nameInput = page.getByLabel('Name').first();
    if (await nameInput.isVisible().catch(() => false)) {
      await nameInput.fill(networkName);
    } else {
      await page.getByRole('textbox', { name: 'Network Name *' }).fill(networkName);
    }
    await page.getByLabel('Create New Network').getByRole('button', { name: 'Create Network' }).click();
    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();

    // 2. Find and remove it
    await navigateToNetworks(page);
    const row = page.locator('tbody tr', { has: page.getByText(networkName) }).first();
    await expect(row).toBeVisible();

    await row.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();

    await expect(page.getByRole('heading', { name: 'Delete Network' })).toBeVisible();
    await page.locator('button:has-text("Delete")').click();
    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
  });

  test('Default networks cannot be removed on details page', async ({ page }) => {
    const bridge = realNetworks.find((n) => n.name === 'bridge');
    await page.goto(`/networks/${bridge.id}`);
    await page.waitForLoadState('networkidle');

    const removeBtn = page.getByRole('button', { name: 'Remove' });
    await expect(removeBtn).toBeDisabled();
  });

  test('Details page shows usage badge', async ({ page }) => {
    await page.goto(`/networks/${realNetworks[0].id}`);
    await page.waitForLoadState('networkidle');

    const inUse = page.locator('text=In Use');
    const unused = page.locator('text=Unused');
    await expect(inUse.first().or(unused.first())).toBeVisible();
  });
});
