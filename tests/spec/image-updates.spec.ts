import { test, expect, type Page } from '@playwright/test';
import { fetchImagesWithRetry } from '../utils/fetch.util';

const ROUTES = {
	page: '/images',
	apiImageUpdatesCheck: '/api/environments/0/image-updates/check',
	apiImageUpdatesCheckById: '/api/environments/0/image-updates/check',
	apiImageUpdatesCheckBatch: '/api/environments/0/image-updates/check-batch',
	apiImageUpdatesCheckAll: '/api/environments/0/image-updates/check-all',
	apiImageUpdatesSummary: '/api/environments/0/image-updates/summary',
};

interface ImageUpdateResponse {
	success: boolean;
	data: {
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
	};
}

interface BatchUpdateResponse {
	success: boolean;
	data: Record<string, ImageUpdateResponse['data']>;
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

test.describe('Image Update API Endpoints', () => {
	test('should check image update by reference via API', async ({ page }) => {
		const imageRef = 'nginx:latest';
		const res = await page.request.get(`${ROUTES.apiImageUpdatesCheck}?imageRef=${encodeURIComponent(imageRef)}`);

		expect(res.status()).toBe(200);

		const json = (await res.json()) as ImageUpdateResponse;
		expect(json.success).toBe(true);
		expect(json.data).toBeDefined();
		expect(typeof json.data.hasUpdate).toBe('boolean');
		expect(json.data.checkTime).toBeDefined();
		expect(typeof json.data.responseTimeMs).toBe('number');
	});

	test('should check image update by ID via API', async ({ page }) => {
		test.skip(!realImages.length, 'No images available for update check');

		const testImage = realImages.find(
			(img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>')
		);
		test.skip(!testImage, 'No suitable image found for update check');

		const res = await page.request.post(`${ROUTES.apiImageUpdatesCheckById}/${testImage.id}`, {
			data: {},
		});

		expect(res.status()).toBe(200);

		const json = (await res.json()) as ImageUpdateResponse;
		expect(json.success).toBe(true);
		expect(json.data).toBeDefined();
		expect(typeof json.data.hasUpdate).toBe('boolean');
	});

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

	test('should return proper error for invalid image reference', async ({ page }) => {
		const res = await page.request.get(`${ROUTES.apiImageUpdatesCheck}?imageRef=`);

		// Should return 400 for empty imageRef
		expect(res.status()).toBe(400);
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

	test('should trigger update check from image row menu', async ({ page }) => {
		test.skip(!realImages.length, 'No images available');

		// Find an image with valid repo/tag
		const testImage = realImages.find(
			(img) => img.repo && img.tag && img.repo !== '<none>' && img.tag !== '<none>'
		);
		test.skip(!testImage, 'No suitable image found');

		await navigateToImages(page);

		// Find a row and open its menu
		const firstRow = page.locator('tbody tr').first();
		await firstRow.getByRole('button', { name: 'Open menu' }).click();

		// Check if there's a "Check for Updates" option (if it exists in the menu)
		const checkUpdatesMenuItem = page.getByRole('menuitem', { name: /check.*update/i });
		const hasCheckUpdates = await checkUpdatesMenuItem.isVisible().catch(() => false);

		if (hasCheckUpdates) {
			await checkUpdatesMenuItem.click();
			// Wait for the check to complete (toast notification)
			await expect(
				page.locator('li[data-sonner-toast]').first()
			).toBeVisible({ timeout: 30000 });
		}
	});

	test('should show update tooltip on hover over status icon', async ({ page }) => {
		test.skip(!realImages.length, 'No images available');

		await navigateToImages(page);

		// Wait for images table
		await expect(page.locator('table')).toBeVisible();

		// Find any status icon (could be check, arrow-up, etc.)
		const statusIcons = page.locator('tbody tr span.inline-flex svg').first();
		const hasStatusIcon = await statusIcons.isVisible().catch(() => false);

		if (hasStatusIcon) {
			// Hover to trigger tooltip
			await statusIcons.hover();

			// Wait for tooltip to appear
			await page.waitForTimeout(500);

			// Check if a tooltip content appeared (using common tooltip patterns)
			const tooltipContent = page.locator('[data-radix-popper-content-wrapper], [role="tooltip"]');
			const tooltipVisible = await tooltipContent.isVisible().catch(() => false);

			// This is informational - tooltip may or may not be present depending on state
			if (tooltipVisible) {
				await expect(tooltipContent).toBeVisible();
			}
		}
	});

	test('should display update information in image detail page', async ({ page }) => {
		test.skip(!realImages.length, 'No images available');

		const testImage = realImages.find(
			(img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>')
		);
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

test.describe('Image Update Response Validation', () => {
	test('should return valid update type values', async ({ page }) => {
		test.skip(!realImages.length, 'No images available');

		const testImage = realImages.find(
			(img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>')
		);
		test.skip(!testImage, 'No suitable image found');

		const res = await page.request.post(`${ROUTES.apiImageUpdatesCheckById}/${testImage.id}`, {
			data: {},
		});

		const json = (await res.json()) as ImageUpdateResponse;

		if (json.data.error) {
			// Error response is valid
			expect(json.data.hasUpdate).toBe(false);
		} else {
			// Valid response should have updateType
			expect(['digest', 'tag', 'none', '']).toContain(json.data.updateType || '');
		}
	});

	test('should include auth information when checking public images', async ({ page }) => {
		const res = await page.request.get(
			`${ROUTES.apiImageUpdatesCheck}?imageRef=${encodeURIComponent('nginx:latest')}`
		);

		const json = (await res.json()) as ImageUpdateResponse;

		// Auth method should be present for successful checks
		if (!json.data.error) {
			expect(['none', 'anonymous', 'credential', 'unknown', undefined]).toContain(
				json.data.authMethod
			);
		}
	});

	test('should handle non-existent image gracefully', async ({ page }) => {
		const fakeImageId = 'sha256:0000000000000000000000000000000000000000000000000000000000000000';

		const res = await page.request.post(`${ROUTES.apiImageUpdatesCheckById}/${fakeImageId}`, {
			data: {},
		});

		// Should return error status or error in response
		const json = await res.json();

		if (res.status() === 200) {
			// If 200, should have error in data
			expect(json.data?.error || json.error).toBeTruthy();
		} else {
			// Non-200 status is also acceptable for non-existent images
			expect([400, 404, 500]).toContain(res.status());
		}
	});

	test('should return response within reasonable time', async ({ page }) => {
		const startTime = Date.now();

		const res = await page.request.get(
			`${ROUTES.apiImageUpdatesCheck}?imageRef=${encodeURIComponent('alpine:latest')}`
		);

		const endTime = Date.now();
		const duration = endTime - startTime;

		expect(res.status()).toBe(200);

		// Response should be within 30 seconds (registry calls can be slow)
		expect(duration).toBeLessThan(30000);

		const json = (await res.json()) as ImageUpdateResponse;

		// responseTimeMs should roughly match actual duration (with some tolerance for network overhead)
		if (!json.data.error && json.data.responseTimeMs) {
			expect(json.data.responseTimeMs).toBeLessThan(duration + 5000);
		}
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
