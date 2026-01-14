import { test, expect, type Page } from '@playwright/test';
import { fetchVolumeCountsWithRetry, fetchVolumesWithRetry } from '../utils/fetch.util';
import { VolumeUsageCounts } from 'types/volumes.type';

let realVolumes: any[] = [];
let volumeCount: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

test.beforeEach(async ({ page }) => {
  await page.goto('/volumes');
  realVolumes = await fetchVolumesWithRetry(page);
  volumeCount = await fetchVolumeCountsWithRetry(page);
});

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
    test.skip(!realVolumes.length, 'No volumes available for inspect test');

    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    const firstRow = page.locator('tbody tr').first();
    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Inspect' }).click();

    await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
  });

  test('Remove Volume', async ({ page }) => {
    const volumeName = `test-remove-volume-${Date.now()}`;
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    // 1. Create the volume first
    await page.getByRole('button', { name: 'Create Volume' }).first().click();
    await page.locator('input[id="volume-name-*"]').fill(volumeName);
    await page.getByRole('dialog').getByRole('button', { name: 'Create Volume' }).click();
    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();

    // 2. Find and remove it
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    const row = await page.getByRole('row', { name: volumeName });
    await row.getByRole('button', { name: 'Open menu' }).click();

    await page.getByRole('menuitem', { name: 'Remove' }).click();

    await expect(page.getByRole('heading', { name: 'Remove Volume' })).toBeVisible();

    await page.locator('button:has-text("Remove")').click();

    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
  });

  test('Create Volume', async ({ page }) => {
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: 'Create Volume' }).first().click();
    await expect(page.getByRole('dialog')).toBeVisible();

    const volumeName = `test-volume-${Date.now()}`;
    await page.locator('input[id="volume-name-*"]').fill(volumeName);

    await page.getByRole('dialog').getByRole('button', { name: 'Create Volume' }).click();

    await expect(
      page.locator(`li[data-sonner-toast][data-type="success"] div[data-title]:has-text("created successfully")`),
    ).toBeVisible();
  });

  test('Display correct volume usage badge', async ({ page }) => {
    await page.goto('/volumes');
    await page.waitForLoadState('networkidle');

    const usedVolumes = realVolumes.filter((vol) => vol.inUse);
    const unusedVolumes = realVolumes.filter((vol) => !vol.inUse);

    if (usedVolumes.length > 0) {
      await expect(page.locator('text="In Use"').first()).toBeVisible();
    }

    if (unusedVolumes.length > 0) {
      await expect(page.locator('text="Unused"').first()).toBeVisible();
    }
  });
});
