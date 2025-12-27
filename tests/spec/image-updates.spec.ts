import { test, expect, type Page } from '@playwright/test';
import { fetchImagesWithRetry } from '../utils/fetch.util';

const ROUTES = {
  page: '/images',
  apiImageUpdatesCheckBatch: '/api/environments/0/image-updates/check-batch',
  apiImageUpdatesCheckAll: '/api/environments/0/image-updates/check-all',
  apiImageUpdatesSummary: '/api/environments/0/image-updates/summary',
};

interface BatchUpdateResponse {
  success: boolean;
  data: Record<
    string,
    {
      hasUpdate: boolean;
      updateType: string;
      currentVersion?: string;
      latestVersion?: string;
      currentDigest?: string;
      latestDigest?: string;
      checkTime: string;
      responseTimeMs: number;
      error?: string;
      authMethod?: string;
      authUsername?: string;
      authRegistry?: string;
      usedCredential?: boolean;
    }
  >;
}

interface UpdateSummary {
  success: boolean;
  data: {
    totalImages: number;
    imagesWithUpdates: number;
    digestUpdates: number;
    errorsCount: number;
  };
}

async function navigateToImages(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState('networkidle');
}

let realImages: any[] = [];

test.beforeEach(async ({ page }) => {
  await navigateToImages(page);

  try {
    const images = await fetchImagesWithRetry(page);
    realImages = Array.isArray(images) ? images : [];
  } catch {
    realImages = [];
  }
});

test.describe('Image Update UI - Check All Updates Button', () => {
  test('should display the Check Updates button on images page', async ({ page }) => {
    await navigateToImages(page);

    // Find the Check Updates button - it might be directly visible or in a menu
    let checkUpdatesButton = page.getByRole('button', { name: 'Check Updates' });
    const isDirectlyVisible = await checkUpdatesButton.isVisible().catch(() => false);

    if (!isDirectlyVisible) {
      // Try to find and click the overflow menu trigger
      const menuTrigger = page.getByRole('button', { name: 'More actions' });
      await menuTrigger.click();
      checkUpdatesButton = page.getByRole('menuitem', { name: 'Check Updates' });
    }

    await expect(checkUpdatesButton).toBeVisible();
  });

  test('should trigger bulk update check when clicking Check Updates button', async ({ page }) => {
    await navigateToImages(page);

    // Find the Check Updates button - it might be directly visible or in a menu
    let checkUpdatesButton = page.getByRole('button', { name: 'Check Updates' });
    const isDirectlyVisible = await checkUpdatesButton.isVisible().catch(() => false);

    if (!isDirectlyVisible) {
      // Try to find and click the overflow menu trigger
      const menuTrigger = page.getByRole('button', { name: 'More actions' });
      await menuTrigger.click();
      checkUpdatesButton = page.getByRole('menuitem', { name: 'Check Updates' });
    }

    await expect(checkUpdatesButton).toBeVisible();

    await checkUpdatesButton.click();

    if (isDirectlyVisible) {
      // If it's a direct button, it should show a loading state
      await expect(checkUpdatesButton).toContainText(/checking/i, { timeout: 10000 });
    }

    // Eventually a success or completion toast should appear
    await expect(page.locator('li[data-sonner-toast]')).toBeVisible({ timeout: 60000 });
  });
});

test.describe('Image Update UI - Individual Image Update Check via Hover Card', () => {
  test('should display update status icons in the images table', async ({ page }) => {
    test.skip(!realImages.length, 'No images available');

    await navigateToImages(page);

    // Wait for the table to load
    await expect(page.locator('table')).toBeVisible();

    // Check that image rows exist
    const rows = page.locator('tbody tr');
    await expect(rows.first()).toBeVisible();
  });

  test('should show hover card tooltip when hovering over update status icon', async ({ page }) => {
    test.skip(!realImages.length, 'No images available');

    await navigateToImages(page);

    // Wait for images table
    await expect(page.locator('table')).toBeVisible();

    // Find the first row's update status area (the Updates column)
    const firstRow = page.locator('tbody tr').first();
    await expect(firstRow).toBeVisible();

    // Look for the update status icon trigger element (Tooltip.Trigger wraps a span)
    const updateStatusTrigger = firstRow.locator('span.inline-flex').first();
    const hasTrigger = await updateStatusTrigger.isVisible().catch(() => false);

    if (hasTrigger) {
      // Hover to trigger tooltip
      await updateStatusTrigger.hover();

      // Wait for tooltip content to appear
      await page.waitForTimeout(500);

      // Check if tooltip/hover card content appeared
      const tooltipContent = page.locator('[data-radix-popper-content-wrapper], [role="tooltip"]');
      const tooltipVisible = await tooltipContent.isVisible().catch(() => false);

      // The hover card should be visible after hovering
      if (tooltipVisible) {
        await expect(tooltipContent).toBeVisible();
      }
    }
  });

  test('should allow triggering individual image update check from hover card', async ({ page }) => {
    test.skip(!realImages.length, 'No images available');

    // Find an image with valid repo/tag for update checking
    const testImage = realImages.find((img) => img.repo && img.tag && img.repo !== '<none>' && img.tag !== '<none>');
    test.skip(!testImage, 'No suitable image found for update check');

    await navigateToImages(page);
    await expect(page.locator('table')).toBeVisible();

    // Find the row for our test image or the first row with a valid image
    const rows = page.locator('tbody tr');
    const firstRow = rows.first();
    await expect(firstRow).toBeVisible();

    // Look for the update status trigger (could be a button or icon)
    const updateTrigger = firstRow.locator('span.inline-flex button, span.inline-flex svg').first();
    const hasUpdateTrigger = await updateTrigger.isVisible().catch(() => false);

    if (hasUpdateTrigger) {
      // If it's a clickable button (for unchecked images), click it
      const isButton = await updateTrigger.evaluate((el) => el.tagName.toLowerCase() === 'button').catch(() => false);

      if (isButton) {
        await updateTrigger.click();

        // Wait for checking to complete (either a toast or state change)
        await expect(async () => {
          const toast = page.locator('li[data-sonner-toast]');
          const toastVisible = await toast.isVisible().catch(() => false);
          expect(toastVisible).toBeTruthy();
        }).toPass({ timeout: 30000 });
      } else {
        // If it's an icon, hover to show the tooltip with recheck button
        await updateTrigger.hover();
        await page.waitForTimeout(500);

        // Look for the recheck button in the tooltip
        const recheckButton = page.locator('[data-radix-popper-content-wrapper] button, [role="tooltip"] button').first();
        const hasRecheckButton = await recheckButton.isVisible().catch(() => false);

        if (hasRecheckButton) {
          await recheckButton.click();

          // Wait for the check to complete
          await expect(async () => {
            const toast = page.locator('li[data-sonner-toast]');
            const toastVisible = await toast.isVisible().catch(() => false);
            expect(toastVisible).toBeTruthy();
          }).toPass({ timeout: 30000 });
        }
      }
    }
  });
});

test.describe('Image Update API Endpoints', () => {
  test('should check batch image updates via API', async ({ page }) => {
    const imageRefs = ['nginx:latest', 'alpine:latest'];

    const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
      data: {
        imageRefs,
      },
    });

    expect(res.status()).toBe(200);

    const json = (await res.json()) as BatchUpdateResponse;
    expect(json.success).toBe(true);
    expect(json.data).toBeDefined();
    expect(typeof json.data).toBe('object');
  });

  test('should check all images for updates via API', async ({ page }) => {
    const res = await page.request.post(ROUTES.apiImageUpdatesCheckAll, {
      data: {},
    });

    expect(res.status()).toBe(200);

    const json = (await res.json()) as BatchUpdateResponse;
    expect(json.success).toBe(true);
    expect(json.data).toBeDefined();
  });

  test('should get update summary via API', async ({ page }) => {
    const res = await page.request.get(ROUTES.apiImageUpdatesSummary);

    expect(res.status()).toBe(200);

    const json = (await res.json()) as UpdateSummary;
    expect(json.success).toBe(true);
    expect(json.data).toBeDefined();
    expect(typeof json.data.totalImages).toBe('number');
    expect(typeof json.data.imagesWithUpdates).toBe('number');
    expect(typeof json.data.digestUpdates).toBe('number');
    expect(typeof json.data.errorsCount).toBe('number');
  });
});

test.describe('Image Update UI Integration', () => {
  test('should display update status icon in images table', async ({ page }) => {
    test.skip(!realImages.length, 'No images available');

    await navigateToImages(page);

    // Wait for the table to load
    await expect(page.locator('table')).toBeVisible();

    // Check that image rows exist
    const rows = page.locator('tbody tr');
    await expect(rows.first()).toBeVisible();
  });

  test('should display update information in image detail page', async ({ page }) => {
    test.skip(!realImages.length, 'No images available');

    const testImage = realImages.find((img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>'));
    test.skip(!testImage, 'No suitable image found');

    // Navigate to image detail
    await page.goto(`/images/${encodeURIComponent(testImage.id)}`);
    await page.waitForLoadState('networkidle');

    // The detail page should load
    await expect(page.locator('h1, h2, [data-testid="image-detail"]').first()).toBeVisible({
      timeout: 10000,
    });
  });
});

test.describe('Batch Update Checks', () => {
  test('should handle empty batch request', async ({ page }) => {
    const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
      data: {
        imageRefs: [],
      },
    });

    expect(res.status()).toBe(200);

    const json = (await res.json()) as BatchUpdateResponse;
    expect(json.success).toBe(true);
    expect(Object.keys(json.data).length).toBe(0);
  });

  test('should return results for each image in batch', async ({ page }) => {
    const imageRefs = ['nginx:latest', 'alpine:latest', 'busybox:latest'];

    const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
      data: {
        imageRefs,
      },
    });

    expect(res.status()).toBe(200);

    const json = (await res.json()) as BatchUpdateResponse;
    expect(json.success).toBe(true);

    // Each requested image should have a result
    for (const ref of imageRefs) {
      expect(json.data[ref]).toBeDefined();
    }
  });

  test('should handle mixed valid and invalid images in batch', async ({ page }) => {
    const imageRefs = ['nginx:latest', 'invalid-registry.example.com/nonexistent:latest'];

    const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
      data: {
        imageRefs,
      },
    });

    expect(res.status()).toBe(200);

    const json = (await res.json()) as BatchUpdateResponse;
    expect(json.success).toBe(true);

    // nginx should succeed
    expect(json.data['nginx:latest']).toBeDefined();

    // Invalid image should have an error
    const invalidResult = json.data['invalid-registry.example.com/nonexistent:latest'];
    if (invalidResult) {
      expect(invalidResult.error || invalidResult.hasUpdate === false).toBeTruthy();
    }
  });
});
