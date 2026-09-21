import { test, expect, type Locator, type Page } from '../fixtures/test.fixture';
import { fetchImagesWithRetry, readList } from '../utils/fetch.util';

const ROUTES = {
	page: '/images',
	apiImageUpdatesCheckBatch: '/api/environments/0/image-updates/check-batch',
	apiImageUpdatesCheckAll: '/api/environments/0/image-updates/check-all',
	apiImageUpdatesSummary: '/api/environments/0/image-updates/summary'
};

const TEST_IMAGE_REFS = {
	nginx: 'public.ecr.aws/nginx/nginx:stable-alpine',
	alpine: 'public.ecr.aws/docker/library/alpine:3.20',
	busybox: 'public.ecr.aws/docker/library/busybox:1.37'
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

	const result = await readList(
		await page.request.get(`/api/environments/0/images?${params.toString()}`),
		'List filtered images'
	);
	expect(result.pagination?.totalItems, 'Image list must include a total').toBeDefined();
	return result.pagination!.totalItems!;
}

async function getCheckUpdatesAction(page: Page): Promise<Locator> {
	await expect(page.getByRole('heading', { name: 'Images', exact: true })).toBeVisible();

	const directButton = page
		.getByRole('button', { name: 'Check Updates', exact: true })
		.filter({ visible: true })
		.first();

	const menuTrigger = page
		.getByRole('button', { name: 'More actions' })
		.filter({ visible: true })
		.first();
	await expect(directButton.or(menuTrigger).first()).toBeVisible();
	if (await directButton.isVisible()) return directButton;

	await menuTrigger.click();

	const menu = page.getByRole('menu').filter({ visible: true }).last();
	await expect(menu).toBeVisible();
	const menuItem = menu.getByRole('menuitem', { name: 'Check Updates', exact: true }).first();
	await expect(menuItem).toBeVisible();
	return menuItem;
}

async function openImageUpdateCard(page: Page, trigger: Locator): Promise<Locator> {
	const usesTouchPopover = await page.evaluate(() => window.matchMedia('(hover: none)').matches);
	if (usesTouchPopover) {
		await trigger.click();
	} else {
		await trigger.hover();
	}

	const content = page.locator('[data-open="true"]').filter({ visible: true }).last();
	await expect(content).toBeVisible();
	return content;
}

let realImages: any[] = [];

test.beforeEach(async ({ page }) => {
	await navigateToImages(page);

	realImages = await fetchImagesWithRetry(page);
});

test.describe('Image Update UI - Check All Updates Button', () => {
	test('should display the Check Updates button on images page', async ({ page }) => {
		await navigateToImages(page);

		const checkUpdatesButton = await getCheckUpdatesAction(page);
		await expect(checkUpdatesButton).toBeVisible();
	});

	test('should trigger bulk update check when clicking Check Updates button', async ({ page }) => {
		await page.route(`**${ROUTES.apiImageUpdatesCheckAll}`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, data: {} })
			});
		});
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
		await expect(
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
		).toBeVisible({ timeout: 60000 });
	});
});

test.describe('Image Update UI - Individual Image Update Check via Hover Card', () => {
	test('should display update status icons in the images table', async ({ page }) => {
		expect(realImages.length, 'No images available').toBeGreaterThan(0);

		await navigateToImages(page);

		// Wait for the table to load
		await expect(page.getByRole('table')).toBeVisible();

		// Check that image rows exist
		const rows = page.getByRole('table').getByRole('row');
		await expect(rows.nth(1)).toBeVisible();
	});

	test('should show hover card tooltip when hovering over update status icon', async ({ page }) => {
		expect(realImages.length, 'No images available').toBeGreaterThan(0);

		await navigateToImages(page);

		// Wait for images table
		await expect(page.getByRole('table')).toBeVisible();

		// Find the first row's update status area (the Updates column)
		const firstRow = page
			.getByRole('row')
			.filter({ has: page.getByTestId('image-update-trigger') })
			.first();
		await expect(firstRow).toBeVisible();

		// Look for the update status icon trigger element (Tooltip.Trigger wraps a span)
		const updateStatusTrigger = firstRow.getByTestId('image-update-trigger').first();
		await openImageUpdateCard(page, updateStatusTrigger);
	});

	test('should allow triggering individual image update check from hover card', async ({
		page
	}) => {
		expect(realImages.length, 'No images available').toBeGreaterThan(0);

		const fixtures = await readList<{ id: string; repoTags: string[] }>(
			await page.request.get('/api/environments/0/images', {
				params: { search: TEST_IMAGE_REFS.busybox }
			}),
			'Find BusyBox fixture'
		);
		const testImage = fixtures.data.find((image) =>
			image.repoTags?.includes(TEST_IMAGE_REFS.busybox)
		);
		expect(testImage, 'BusyBox fixture must be present').toBeDefined();
		await navigateToImages(page);
		const filteredImagesResponse = page.waitForResponse((response) => {
			const url = new URL(response.url());
			return (
				response.request().method() === 'GET' &&
				url.pathname === '/api/environments/0/images' &&
				url.searchParams.get('search') === testImage!.id
			);
		});
		await page.getByPlaceholder('Search…').first().fill(testImage!.id);
		const filteredResponse = await filteredImagesResponse;
		expect(filteredResponse.ok()).toBe(true);
		await filteredResponse.finished();
		await expect(page.getByRole('table').locator('tbody tr')).toHaveCount(1);
		const row = page
			.getByRole('row')
			.filter({ has: page.locator(`a[href="/images/${testImage!.id}"]`) });
		const trigger = row.getByTestId('image-update-trigger');
		await expect(trigger).toBeVisible();
		const responsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				decodeURIComponent(new URL(response.url()).pathname) ===
					`/api/environments/0/image-updates/check/${testImage!.id}`
		);
		if (await trigger.evaluate((element) => element.tagName === 'BUTTON')) {
			await trigger.click();
		} else {
			const updateCard = await openImageUpdateCard(page, trigger);
			await updateCard.getByRole('button', { name: 'Re-check Updates', exact: true }).click();
		}
		expect((await responsePromise).ok()).toBe(true);
		await expect(page.locator('li[data-sonner-toast]').first()).toBeVisible();
	});
});

test.describe('Image Update API Endpoints', () => {
	test('should check batch image updates via API', async ({ page }) => {
		const imageRefs = [TEST_IMAGE_REFS.nginx, TEST_IMAGE_REFS.alpine];

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
		expect(realImages.length, 'No images available').toBeGreaterThan(0);

		await navigateToImages(page);

		// Wait for the table to load
		await expect(page.getByRole('table')).toBeVisible();

		// Check that image rows exist
		const rows = page.getByRole('table').getByRole('row');
		await expect(rows.nth(1)).toBeVisible();
	});

	test('should display update information in image detail page', async ({ page }) => {
		expect(realImages.length, 'No images available').toBeGreaterThan(0);

		const testImage = realImages.find(
			(img) => img.repoTags?.[0] && !img.repoTags[0].includes('<none>')
		);
		expect(testImage, 'No suitable image found').toBeTruthy();

		// Navigate to image detail
		await page.goto(`/images/${encodeURIComponent(testImage.id)}`);
		await page.waitForLoadState('load');

		// The detail page should load
		await expect(page.getByRole('heading').first()).toBeVisible({
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
		const imageRefs = [TEST_IMAGE_REFS.nginx, TEST_IMAGE_REFS.alpine, TEST_IMAGE_REFS.busybox];

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
		const imageRefs = [TEST_IMAGE_REFS.nginx, 'invalid-registry.example.com/nonexistent:latest'];

		const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
			data: {
				imageRefs
			}
		});

		expect(res.status()).toBe(200);

		const json = (await res.json()) as BatchUpdateResponse;
		expect(json.success).toBe(true);

		// The Public ECR image should succeed.
		expect(json.data[TEST_IMAGE_REFS.nginx]).toBeDefined();

		// Invalid image should have an error
		const invalidResult = json.data['invalid-registry.example.com/nonexistent:latest'];
		if (invalidResult) {
			expect(invalidResult.error || invalidResult.hasUpdate === false).toBeTruthy();
		}
	});
});
