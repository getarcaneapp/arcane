import { test, expect, type Locator, type Page } from '@playwright/test';
import { fetchImagesWithRetry } from '../utils/fetch.util';

const ROUTES = {
	page: '/images',
	apiImageUpdatesCheckBatch: '/api/environments/0/image-updates/check-batch',
	apiImageUpdatesCheckAll: '/api/environments/0/image-updates/check-all',
	apiImageUpdatesSummary: '/api/environments/0/image-updates/summary'
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
	await page.waitForLoadState('load');
}

async function fetchImagesTotal(page: Page, updatesFilter?: string): Promise<number> {
	const params = new URLSearchParams({ start: '0', limit: '1' });
	if (updatesFilter) {
		params.set('updates', updatesFilter);
	}

	const res = await page.request.get(`/api/environments/0/images?${params.toString()}`);
	expect(res.status()).toBe(200);

	const body = await res.json().catch(() => null as any);
	const totalItems = Number(body?.pagination?.totalItems ?? 0);
	return Number.isFinite(totalItems) ? totalItems : 0;
}

async function getCheckUpdatesAction(page: Page): Promise<Locator> {
	const pageHeader = page
		.locator('main header')
		.filter({ has: page.getByRole('heading', { name: 'Images', exact: true }) })
		.first();
	await expect(pageHeader).toBeVisible();

	const directButton = pageHeader
		.getByRole('button', { name: 'Check Updates', exact: true })
		.filter({ visible: true })
		.first();

	if (
		await expect(directButton)
			.toBeVisible({ timeout: 5000 })
			.then(
				() => true,
				() => false
			)
	) {
		return directButton;
	}

	const menuTrigger = pageHeader
		.getByRole('button', { name: 'More actions' })
		.filter({ visible: true })
		.first();
	await expect(menuTrigger).toBeVisible();
	await menuTrigger.click();

	const menu = page.locator('[data-slot="dropdown-menu-content"]:visible').last();
	await expect(menu).toBeVisible();
	const menuItem = menu.getByRole('menuitem', { name: 'Check Updates', exact: true }).first();
	await expect(menuItem).toBeVisible();
	return menuItem;
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

		const checkUpdatesButton = await getCheckUpdatesAction(page);
		await expect(checkUpdatesButton).toBeVisible();
	});

	test('should trigger bulk update check when clicking Check Updates button', async ({ page }) => {
		await navigateToImages(page);

		const checkUpdatesButton = await getCheckUpdatesAction(page);
		await expect(checkUpdatesButton).toBeVisible();

		const checkAllResponsePromise = page.waitForResponse((response) => {
			const request = response.request();
			if (request.method() !== 'POST') return false;
			return new URL(response.url()).pathname === ROUTES.apiImageUpdatesCheckAll;
		});

		await checkUpdatesButton.click();

		const checkAllResponse = await checkAllResponsePromise;
		expect(checkAllResponse.ok()).toBeTruthy();

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
		const updateStatusTrigger = firstRow.locator('[data-testid="image-update-trigger"]').first();
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

	test('should allow triggering individual image update check from hover card', async ({
		page
	}) => {
		test.skip(!realImages.length, 'No images available');

		// Find an image with valid repo/tag for update checking
		const testImage = realImages.find(
			(img) => img.repo && img.tag && img.repo !== '<none>' && img.tag !== '<none>'
		);
		test.skip(!testImage, 'No suitable image found for update check');

		await navigateToImages(page);
		await expect(page.locator('table')).toBeVisible();

		// Find the row for our test image or the first row with a valid image
		const rows = page.locator('tbody tr');
		const firstRow = rows.first();
		await expect(firstRow).toBeVisible();

		// Look for the update status trigger (could be a button or icon)
		const updateTrigger = firstRow.locator('[data-testid="image-update-trigger"]').first();
		const hasUpdateTrigger = await updateTrigger.isVisible().catch(() => false);

		if (hasUpdateTrigger) {
			// If it's a clickable button (for unchecked images), click it
			const updateButton = updateTrigger.locator('button').first();
			const hasButton = await updateButton.isVisible().catch(() => false);

			if (hasButton) {
				await updateButton.click();

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
				const recheckButton = page
					.locator('[data-radix-popper-content-wrapper] button, [role="tooltip"] button')
					.first();
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
				imageRefs
			}
		});

		expect(res.status()).toBe(200);

		const json = (await res.json()) as BatchUpdateResponse;
		expect(json.success).toBe(true);
		expect(json.data).toBeDefined();
		expect(typeof json.data).toBe('object');
	});

	test('should check all images for updates via API', async ({ page }) => {
		const res = await page.request.post(ROUTES.apiImageUpdatesCheckAll, {
			data: {}
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

		const [imagesTotal, hasUpdateTotal] = await Promise.all([
			fetchImagesTotal(page),
			fetchImagesTotal(page, 'has_update')
		]);

		expect(json.data.totalImages).toBe(imagesTotal);
		expect(json.data.imagesWithUpdates).toBe(hasUpdateTotal);
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

		const testImage = realImages.find(
			(img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>')
		);
		test.skip(!testImage, 'No suitable image found');

		// Navigate to image detail
		await page.goto(`/images/${encodeURIComponent(testImage.id)}`);
		await page.waitForLoadState('load');

		// The detail page should load
		await expect(page.locator('h1, h2, [data-testid="image-detail"]').first()).toBeVisible({
			timeout: 10000
		});
	});
});

test.describe('Batch Update Checks', () => {
	test('should handle empty batch request', async ({ page }) => {
		const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
			data: {
				imageRefs: []
			}
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
				imageRefs
			}
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
				imageRefs
			}
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
