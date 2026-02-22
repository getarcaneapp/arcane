import { test, expect, type Page } from '@playwright/test';
import { fetchVolumeCountsWithRetry } from '../utils/fetch.util';
import { VolumeUsageCounts } from 'types/volumes.type';

let volumeCount: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

test.beforeEach(async ({ page }) => {
  await page.goto('/volumes');
  volumeCount = await fetchVolumeCountsWithRetry(page);
});

async function createVolumeViaUI(page: Page, volumeName: string) {
  await page.goto('/volumes');
  await page.waitForLoadState('networkidle');
  await page.getByRole('button', { name: 'Create Volume' }).first().click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('dialog').locator('input[type="text"]').first().fill(volumeName);
  await page.getByRole('dialog').getByRole('button', { name: 'Create Volume' }).click();
  await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
}

async function findVolumeRow(page: Page, volumeName: string, maxRetries = 10) {
  for (let i = 0; i < maxRetries; i++) {
    const searchInput = page.getByPlaceholder(/Search/i).first();
    if (await searchInput.isVisible().catch(() => false)) {
      await searchInput.fill(volumeName);
    }

    const row = page.locator('tbody tr').filter({ hasText: volumeName }).first();
    if (await row.isVisible().catch(() => false)) return row;
    await page.waitForTimeout(500);
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');
  }
  return page.locator('tbody tr').filter({ hasText: volumeName }).first();
}

async function removeVolumeViaUI(page: Page, volumeName: string) {
  await page.goto('/volumes');
  await page.waitForLoadState('networkidle');

  const row = await findVolumeRow(page, volumeName, 4);
  if ((await row.count()) === 0) return;

  await row.locator('a[href*="/volumes/"]').first().click();
  await expect(page).toHaveURL(/\/volumes\/.+/);
  await page.getByRole('button', { name: 'Remove', exact: true }).click();
  await page.getByRole('button', { name: 'Remove', exact: true }).last().click();
  await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
}

function facetIds(title: string) {
  const key = title.toLowerCase();
  return {
    triggerId: `facet-${key}-trigger`,
    contentId: `facet-${key}-content`,
  };
}

async function ensureFacetOpen(page: Page, title: string) {
  const { triggerId, contentId } = facetIds(title);
  const trigger = page.getByTestId(triggerId).first();
  const content = page.getByTestId(contentId).first();

  if (await content.isVisible().catch(() => false)) return { trigger, content };

  if ((await trigger.getAttribute('data-state')) !== 'open') await trigger.click();
  await content.waitFor({ state: 'visible' });
  return { trigger, content };
}

test.describe('Volumes Page', () => {
  test('Volume Page Display', async ({ page }) => {
    await page.goto('/volumes');

    await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible();
    await expect(page.getByText('Manage your Docker volumes').first()).toBeVisible();
  });

  test('Correct Volume Stat Card Counts', async ({ page }) => {
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText(`${volumeCount.total} Total Volumes`)).toBeVisible();
  });

  test('Create Volume Sheet Opens', async ({ page }) => {
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: 'Create Volume' }).first().click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Create New Volume')).toBeVisible();
  });

  test('Display Volume Filters', async ({ page }) => {
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    const { content } = await ensureFacetOpen(page, 'Usage');
    await expect(content.getByRole('option', { name: /In Use\b/i })).toBeVisible();
    await expect(content.getByRole('option', { name: /Unused\b/i })).toBeVisible();
  });

  test('Inspect Volume', async ({ page }) => {
    const volumeName = `e2e-inspect-volume-${Date.now()}`;

    try {
      await createVolumeViaUI(page, volumeName);
      await page.goto('/volumes');
      await page.waitForLoadState('networkidle');

      const row = await findVolumeRow(page, volumeName);
      await expect(row).toBeVisible();
      await row.locator('a[href*="/volumes/"]').first().click();

      await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
      await expect(page.getByRole('heading', { level: 1, name: volumeName })).toBeVisible();
    } finally {
      await removeVolumeViaUI(page, volumeName);
    }
  });

  test('Remove Volume', async ({ page }) => {
    const volumeName = `test-remove-volume-${Date.now()}`;
    await createVolumeViaUI(page, volumeName);
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    const row = await findVolumeRow(page, volumeName);
    await expect(row).toBeVisible();
    await row.locator('a[href*="/volumes/"]').first().click();
    await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
    await page.getByRole('button', { name: 'Remove', exact: true }).click();
    await page.getByRole('button', { name: 'Remove', exact: true }).last().click();

    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
  });

  test('Create Volume', async ({ page }) => {
    const volumeName = `test-volume-${Date.now()}`;
    try {
      await createVolumeViaUI(page, volumeName);
      await page.goto('/volumes');
      await expect(await findVolumeRow(page, volumeName)).toBeVisible();
    } finally {
      await removeVolumeViaUI(page, volumeName);
    }
  });

  test('Display correct volume usage badge', async ({ page }) => {
    const volumeName = `e2e-badge-volume-${Date.now()}`;
    try {
      await createVolumeViaUI(page, volumeName);
      await page.goto('/volumes');
      await page.waitForLoadState('networkidle');

      const row = await findVolumeRow(page, volumeName);
      await expect(row).toBeVisible();
      await expect(row.getByText('Unused')).toBeVisible();
    } finally {
      await removeVolumeViaUI(page, volumeName);
    }
  });
});
