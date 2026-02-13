import { test, expect, type Page } from '@playwright/test';
import { fetchNetworksCountsWithRetry } from '../utils/fetch.util';

async function navigateToNetworks(page: Page) {
  await page.goto('/networks');
  await page.waitForLoadState('networkidle');
}

test.beforeEach(async ({ page }) => {
  await navigateToNetworks(page);
});

async function createNetworkViaUI(page: Page, networkName: string) {
  await navigateToNetworks(page);
  await page.getByRole('button', { name: 'Create Network' }).first().click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Create New Network' })).toBeVisible();
  await page.locator('#network-name').fill(networkName);

  const createRequest = page.waitForResponse(
    (response) => {
      const request = response.request();
      if (request.method() !== 'POST') return false;
      return /\/api\/environments\/[^/]+\/networks$/.test(new URL(response.url()).pathname);
    },
    { timeout: 15000 },
  );

  await page.getByRole('dialog').getByRole('button', { name: 'Create Network' }).click();
  const createResponse = await createRequest;
  if (!createResponse.ok()) {
    const responseText = await createResponse.text().catch(() => '');
    throw new Error(`Failed to create network ${networkName}: ${createResponse.status()} ${responseText}`);
  }

  await navigateToNetworks(page);
  await expect(await findNetworkRow(page, networkName, 15)).toBeVisible();
}

async function findNetworkRow(page: Page, networkName: string, maxRetries = 10) {
  for (let i = 0; i < maxRetries; i++) {
    const searchInput = page.getByPlaceholder(/Search/i).first();
    if (await searchInput.isVisible().catch(() => false)) {
      await searchInput.fill(networkName);
    }

    const row = page.locator('tbody tr', { has: page.getByText(networkName) }).first();
    if (await row.isVisible().catch(() => false)) return row;
    await page.waitForTimeout(500);
    await navigateToNetworks(page);
  }
  return page.locator('tbody tr', { has: page.getByText(networkName) }).first();
}

async function removeNetworkViaUI(page: Page, networkName: string) {
  await navigateToNetworks(page);

  const row = await findNetworkRow(page, networkName, 4);
  if ((await row.count()) === 0) return;

  await row.locator('a[href*="/networks/"]').first().click();
  await expect(page).toHaveURL(/\/networks\/.+/);
  await page.getByRole('button', { name: 'Remove', exact: true }).click();
  await page.getByRole('button', { name: 'Remove', exact: true }).last().click();
  await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
}

test.describe('Networks Page', () => {
  test.describe.configure({ mode: 'serial' });

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
    const networkName = `e2e-table-network-${Date.now()}`;
    await navigateToNetworks(page);
    try {
      await createNetworkViaUI(page, networkName);
      await navigateToNetworks(page);
      await expect(page.locator('table')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Name' })).toBeVisible();
      await expect(await findNetworkRow(page, networkName)).toBeVisible();
    } finally {
      await removeNetworkViaUI(page, networkName);
    }
  });

  test('Open Create Network sheet', async ({ page }) => {
    const networkName = `test-network-${Date.now()}`;
    try {
      await createNetworkViaUI(page, networkName);
      await navigateToNetworks(page);
      await expect(await findNetworkRow(page, networkName)).toBeVisible();
    } finally {
      await removeNetworkViaUI(page, networkName);
    }
  });

  test('Inspect Network from row actions', async ({ page }) => {
    const networkName = `e2e-inspect-network-${Date.now()}`;
    try {
      await createNetworkViaUI(page, networkName);
      await navigateToNetworks(page);

      const row = await findNetworkRow(page, networkName);
      await expect(row).toBeVisible();
      await row.locator('a[href*="/networks/"]').first().click();
      await expect(page).toHaveURL(/\/networks\/.+/);
      await expect(page.getByRole('heading', { level: 1, name: networkName })).toBeVisible();
    } finally {
      await removeNetworkViaUI(page, networkName);
    }
  });

  test('Remove Network from table', async ({ page }) => {
    const networkName = `test-remove-network-${Date.now()}`;
    await createNetworkViaUI(page, networkName);
    await navigateToNetworks(page);
    const row = await findNetworkRow(page, networkName);
    await expect(row).toBeVisible();

    await row.locator('a[href*="/networks/"]').first().click();
    await expect(page).toHaveURL(/\/networks\/.+/);
    await page.getByRole('button', { name: 'Remove', exact: true }).click();
    await page.getByRole('button', { name: 'Remove', exact: true }).last().click();
    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();

    await navigateToNetworks(page);
    const removedRow = await findNetworkRow(page, networkName, 2);
    await expect(removedRow).not.toBeVisible();
  });

  test('Default networks cannot be removed on details page', async ({ page }) => {
    await navigateToNetworks(page);
    const bridgeRow = page.locator('tbody tr', { has: page.getByText('bridge', { exact: true }) }).first();
    await expect(bridgeRow).toBeVisible();
    await bridgeRow.locator('a[href*="/networks/"]').first().click();
    await page.waitForLoadState('networkidle');

    const removeBtn = page.getByRole('button', { name: 'Remove' });
    await expect(removeBtn).toBeDisabled();
  });

  test('Details page shows usage badge', async ({ page }) => {
    const networkName = `e2e-badge-network-${Date.now()}`;
    try {
      await createNetworkViaUI(page, networkName);
      await navigateToNetworks(page);
      const row = await findNetworkRow(page, networkName);
      await expect(row).toBeVisible();
      await row.locator('a[href*="/networks/"]').first().click();
      await page.waitForLoadState('networkidle');

      await expect(page.getByText('Unused').first()).toBeVisible();
    } finally {
      await removeNetworkViaUI(page, networkName);
    }
  });
});
