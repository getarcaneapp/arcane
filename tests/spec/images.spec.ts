import { test, expect, type Page } from '@playwright/test';
import { fetchImageCountsWithRetry, fetchImagesWithRetry } from '../utils/fetch.util';
import { ImageUsageCounts } from 'types/image.type';

const ROUTES = {
  page: '/images',
  apiImages: '/api/environments/0/images',
};

async function navigateToImages(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState('networkidle');
}

let realImages: any[] = [];
let imageCounts: ImageUsageCounts = { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 };

test.beforeEach(async ({ page }) => {
  await navigateToImages(page);

  try {
    const images = await fetchImagesWithRetry(page);
    realImages = Array.isArray(images) ? images : [];
    imageCounts = await fetchImageCountsWithRetry(page);
  } catch {
    realImages = [];
  }
});

test.describe('Images Page', () => {
  test('should display the images page title and description', async ({ page }) => {
    await navigateToImages(page);

    await expect(page.getByRole('heading', { name: 'Images', level: 1 })).toBeVisible();
    await expect(page.getByText('View and Manage your Container images').first()).toBeVisible();
  });

  test('should display stats cards with correct counts and size', async ({ page }) => {
    await navigateToImages(page);

    await expect(page.getByText(`${imageCounts.totalImages} Total Images`)).toBeVisible();
    await expect(page.getByText(/Total Size/i)).toBeVisible();
  });

  test('should display the image table when images exist', async ({ page }) => {
    await navigateToImages(page);

    await expect(page.locator('table')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Repository' })).toBeVisible();
  });

  test('should open the Pull Image dialog', async ({ page }) => {
    await navigateToImages(page);
    await page.getByRole('button', { name: 'Pull Image' }).click();
    await expect(page.locator('div[role="heading"][aria-level="2"][data-dialog-title]:has-text("Pull Image")')).toBeVisible();
  });

  test('should open the Prune Unused Images dialog', async ({ page }) => {
    await navigateToImages(page);

    let pruneButton = page.getByRole('button', { name: 'Prune Unused' });
    const isDirectlyVisible = await pruneButton.isVisible().catch(() => false);

    if (!isDirectlyVisible) {
      await page.getByRole('button', { name: 'More actions' }).click();
      pruneButton = page.getByRole('menuitem', { name: 'Prune Unused' });
    }

    await pruneButton.click();
    await expect(
      page.locator('div[role="heading"][aria-level="2"][data-dialog-title]:has-text("Prune Unused Images")'),
    ).toBeVisible();
  });

  test('should navigate to image details on inspect click', async ({ page }) => {
    await navigateToImages(page);

    const firstRow = page.locator('tbody tr').first();
    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Inspect' }).click();
  });

  test('should pull image from dropdown menu', async ({ page }) => {
    test.skip(!realImages.length, 'No images available for pull API test');
    await navigateToImages(page);

    const firstRow = await page.getByRole('row', { name: 'ghcr.io/linuxserver/nginx' });
    await firstRow.getByLabel('Open menu').click();
    await page.getByRole('menuitem', { name: 'Pull' }).click();

    await page.waitForLoadState('networkidle');

    await expect(page.locator(`li[data-sonner-toast][data-type="success"] div[data-title]`)).toBeVisible();
  });

  test('should call remove API on row action remove click and confirmation', async ({ page }) => {
    test.skip(!realImages.length, 'No images available for remove API test');
    await navigateToImages(page);

    const deleteableImage = realImages.find((img) => img.repoTags?.[0]?.includes('ghcr.io/linuxserver/radarr'));
    test.skip(!deleteableImage, 'No deletable images available');

    const firstRow = await page.getByRole('row', { name: 'ghcr.io/linuxserver/radarr' });
    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Remove' }).click();

    await expect(page.locator('div[role="heading"][aria-level="2"][data-dialog-title]:has-text("Remove Image")')).toBeVisible();

    await page.locator('button:has-text("Remove")').click();

    await expect(page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')).toBeVisible();
  });

  test('should call prune API on prune click and confirmation', async ({ page }) => {
    await navigateToImages(page);

    let pruneButton = page.getByRole('button', { name: 'Prune Unused' });
    const isDirectlyVisible = await pruneButton.isVisible().catch(() => false);

    if (!isDirectlyVisible) {
      await page.getByRole('button', { name: 'More actions' }).click();
      pruneButton = page.getByRole('menuitem', { name: 'Prune Unused' });
    }

    await pruneButton.click();

    await expect(
      page.locator('div[role="heading"][aria-level="2"][data-dialog-title]:has-text("Prune Unused Images")'),
    ).toBeVisible();

    await page.locator('button:has-text("Prune Images")').click();

    await expect(page.locator(`li[data-sonner-toast][data-type="success"] div[data-title]:has-text("pruned")`)).toBeVisible({
      timeout: 10000,
    });
  });

  test('should pull image via form', async ({ page }) => {
    await navigateToImages(page);

    await page.getByRole('button', { name: 'Pull Image' }).click();
    const dialogHeading = page.getByRole('heading', { name: 'Pull Image' });
    await expect(dialogHeading).toBeVisible();

    await page.getByRole('textbox', { name: 'Image Name *' }).fill('ghcr.io/linuxserver/nginx');
    await page.getByRole('textbox', { name: 'Tag' }).fill('1.28.0');

    await page.getByRole('button', { name: 'Pull', exact: true }).click();

    await expect(dialogHeading).toBeHidden({ timeout: 120_000 });
  });
});
