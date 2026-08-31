import { test, expect, type Page } from '../fixtures/test.fixture';
import { fetchImageCountsWithRetry, fetchImagesWithRetry } from '../utils/fetch.util';
import { ImageUsageCounts } from 'types/image.type';
import { openRowActionsMenu } from '../utils/table-actions.util';

const ROUTES = {
	page: '/images',
	apiImages: '/api/environments/0/images'
};

async function navigateToImages(page: Page) {
	await page.goto(ROUTES.page);
	await page.waitForLoadState('load');
}

function getImageRows(page: Page) {
	return page
		.getByRole('row')
		.filter({ has: page.getByRole('checkbox', { name: 'Select row', exact: true }) });
}

async function mockImageDeleteFlow(page: Page, failFirst = false) {
	const state = {
		deleteRequestCount: 0,
		imageListRequestCount: 0,
		usageCountRequestCount: 0,
		inFlight: 0,
		maxInFlight: 0,
		deletedIds: new Set<string>()
	};

	await page.route('**/api/environments/0/images**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());

		if (request.method() === 'DELETE' && url.pathname.startsWith(`${ROUTES.apiImages}/`)) {
			state.deleteRequestCount += 1;
			state.inFlight += 1;
			state.maxInFlight = Math.max(state.maxInFlight, state.inFlight);
			const imageId = decodeURIComponent(url.pathname.slice(`${ROUTES.apiImages}/`.length));
			const shouldFail = failFirst && state.deleteRequestCount === 1;

			await new Promise((resolve) => setTimeout(resolve, 50));
			state.inFlight -= 1;
			if (!shouldFail) state.deletedIds.add(imageId);

			await route.fulfill({
				status: shouldFail ? 500 : 200,
				contentType: 'application/json',
				body: JSON.stringify(
					shouldFail
						? { success: false, message: 'mock delete failure' }
						: { success: true, data: { message: 'Image removed successfully' } }
				)
			});
			return;
		}

		if (request.method() === 'GET' && url.pathname === ROUTES.apiImages) {
			state.imageListRequestCount += 1;
			const response = await route.fetch();
			const body = await response.json();
			if (Array.isArray(body?.data)) {
				body.data = body.data.filter((image: { id: string }) => !state.deletedIds.has(image.id));
				if (body.pagination?.totalItems !== undefined) {
					body.pagination.totalItems = Math.max(
						0,
						Number(body.pagination.totalItems) - state.deletedIds.size
					);
				}
			}
			await route.fulfill({ response, body: JSON.stringify(body) });
			return;
		}

		if (request.method() === 'GET' && url.pathname === `${ROUTES.apiImages}/counts`) {
			state.usageCountRequestCount += 1;
		}

		await route.continue();
	});

	return state;
}

async function fetchAllImagesForUsage(page: Page): Promise<any[]> {
	const limit = 200;
	let start = 0;
	const all: any[] = [];

	while (true) {
		const res = await page.request.get(`${ROUTES.apiImages}?start=${start}&limit=${limit}`);
		if (!res.ok()) {
			throw new Error(`HTTP ${res.status()}`);
		}

		const body = await res.json().catch(() => null as any);
		const data = Array.isArray(body?.data) ? body.data : [];
		all.push(...data);

		const totalItems = Number(body?.pagination?.totalItems ?? all.length);
		if (data.length === 0 || all.length >= totalItems) {
			break;
		}

		start += limit;
	}

	return all;
}

let realImages: any[] = [];
let imageCounts: ImageUsageCounts = {
	imagesInuse: 0,
	imagesUnused: 0,
	totalImages: 0,
	totalImageSize: 0
};

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
		await expect(page.getByText('Total Size', { exact: true })).toBeVisible();
	});

	test('should align /images/counts with usage derived from /images list', async ({ page }) => {
		await navigateToImages(page);

		await expect
			.poll(
				async () => {
					const [allImages, counts] = await Promise.all([
						fetchAllImagesForUsage(page),
						fetchImageCountsWithRetry(page)
					]);
					const derivedInUse = allImages.filter((image) => !!image.inUse).length;
					return {
						totalDifference: counts.totalImages - allImages.length,
						inUseDifference: counts.imagesInuse - derivedInUse,
						unusedDifference: counts.imagesUnused - (allImages.length - derivedInUse)
					};
				},
				{
					message:
						'Expected image usage counts and the image list to describe the same Docker state',
					timeout: 30_000,
					intervals: [500, 1_000, 2_000]
				}
			)
			.toEqual({ totalDifference: 0, inUseDifference: 0, unusedDifference: 0 });
	});

	test('should display the image table when images exist', async ({ page }) => {
		await navigateToImages(page);

		await expect(page.getByRole('table')).toBeVisible();
		await expect(page.getByRole('button', { name: 'Repository' })).toBeVisible();
	});

	test('should open the Pull Image dialog', async ({ page }) => {
		await navigateToImages(page);
		await page.getByRole('button', { name: 'Pull Image' }).click();
		await expect(page.getByRole('heading', { name: 'Pull Image', exact: true })).toBeVisible();
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
			page.getByRole('heading', { name: 'Prune Unused Images', exact: true })
		).toBeVisible();
	});

	test('should navigate to image details on inspect click', async ({ page }) => {
		await navigateToImages(page);

		const firstRow = page
			.getByRole('row')
			.filter({ has: page.getByRole('button', { name: 'Open menu', exact: true }) })
			.first();
		const menu = await openRowActionsMenu(page, firstRow);
		await menu.getByRole('menuitem', { name: 'Inspect' }).click();
	});

	test('should pull image from dropdown menu', async ({ page }) => {
		test.skip(!realImages.length, 'No images available for pull API test');
		await navigateToImages(page);

		const firstRow = page
			.getByRole('row')
			.filter({ has: page.getByRole('button', { name: 'Open menu', exact: true }) })
			.first();
		const menu = await openRowActionsMenu(page, firstRow);
		await menu.getByRole('menuitem', { name: 'Pull' }).click();

		await page.waitForLoadState('load');

		await expect(
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
		).toBeVisible();
	});

	test('should call remove API on row action remove click and confirmation', async ({ page }) => {
		test.skip(!realImages.length, 'No images available for remove API test');
		await navigateToImages(page);

		const removableImage = realImages.find((image) => image.repo && image.repo !== '<none>');
		test.skip(!removableImage, 'No removable images available');
		const removePath = `/api/environments/0/images/${removableImage.id}`;

		let removeRequestCount = 0;
		await page.route(
			(url) => decodeURIComponent(url.pathname) === removePath,
			async (route) => {
				if (route.request().method() !== 'DELETE') {
					await route.continue();
					return;
				}

				removeRequestCount += 1;
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: { message: 'Image removed successfully' }
					})
				});
			}
		);

		const imageRow = page
			.getByRole('row')
			.filter({
				has: page.getByRole('link', { name: removableImage.repo, exact: true })
			})
			.first();
		const menu = await openRowActionsMenu(page, imageRow);
		await menu.getByRole('menuitem', { name: 'Remove', exact: true }).click();

		const dialog = page.getByRole('dialog', { name: 'Remove image', exact: true });
		await expect(dialog).toBeVisible();
		// The post-remove refresh goes through queryClient.fetchQuery, which joins an
		// already in-flight background refetch instead of issuing a new request, so
		// asserting on fresh network calls here races under parallel load. The bulk
		// removal tests below cover the refresh path with stubbed list/count endpoints.
		await dialog.getByRole('button', { name: 'Remove', exact: true }).click();
		await expect.poll(() => removeRequestCount).toBe(1);

		await expect(
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
		).toBeVisible();
	});

	test('should remove selected images sequentially and refresh the list and usage counts', async ({
		page
	}) => {
		test.skip(realImages.length < 2, 'At least two images are required for bulk remove test');
		await navigateToImages(page);

		const rows = getImageRows(page);
		await expect(rows.nth(1)).toBeVisible();
		const initialRowCount = await rows.count();
		const mock = await mockImageDeleteFlow(page);

		await rows.nth(0).getByRole('checkbox', { name: 'Select row', exact: true }).check();
		await rows.nth(1).getByRole('checkbox', { name: 'Select row', exact: true }).check();
		await expect(
			page.getByRole('button', { name: 'Remove Selected (2)', exact: true })
		).toBeVisible();
		await page.getByRole('button', { name: 'Remove Selected (2)', exact: true }).click();

		const dialog = page.getByRole('dialog');
		await dialog.getByRole('button', { name: 'Remove', exact: true }).click();

		await expect.poll(() => mock.deleteRequestCount).toBe(2);
		expect(mock.maxInFlight).toBe(1);
		await expect(
			page
				.getByRole('region', { name: 'Notifications alt+T', exact: true })
				.getByRole('listitem')
				.filter({ hasText: '2 images removed successfully' })
		).toBeVisible();
		await expect.poll(() => getImageRows(page).count()).toBe(initialRowCount - 2);
		await expect(page.getByRole('button', { name: /^Remove Selected/ })).toHaveCount(0);
	});

	test('should continue bulk removal after a failed image and refresh successful results', async ({
		page
	}) => {
		test.skip(realImages.length < 2, 'At least two images are required for bulk remove test');
		await navigateToImages(page);

		const rows = getImageRows(page);
		await expect(rows.nth(1)).toBeVisible();
		const initialRowCount = await rows.count();
		const mock = await mockImageDeleteFlow(page, true);

		await rows.nth(0).getByRole('checkbox', { name: 'Select row', exact: true }).check();
		await rows.nth(1).getByRole('checkbox', { name: 'Select row', exact: true }).check();
		await page.getByRole('button', { name: 'Remove Selected (2)', exact: true }).click();

		const dialog = page.getByRole('dialog');
		await dialog.getByRole('button', { name: 'Remove', exact: true }).click();

		await expect.poll(() => mock.deleteRequestCount).toBe(2);
		expect(mock.maxInFlight).toBe(1);
		await expect(
			page
				.getByRole('region', { name: 'Notifications alt+T', exact: true })
				.getByRole('listitem')
				.filter({ hasText: 'Removed 1 of 2' })
		).toBeVisible();
		await expect.poll(() => getImageRows(page).count()).toBe(initialRowCount - 1);
		await expect(page.getByRole('button', { name: /^Remove Selected/ })).toHaveCount(0);
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

		const dialog = page.getByRole('dialog');
		await expect(
			dialog.getByRole('heading', { name: 'Prune Unused Images', exact: true })
		).toBeVisible();
		await dialog.getByRole('button', { name: 'Prune Images', exact: true }).click();

		await expect(
			page
				.getByRole('region', { name: 'Notifications alt+T', exact: true })
				.getByRole('listitem')
				.filter({ hasText: 'pruned' })
		).toBeVisible({
			timeout: 10000
		});
	});

	test('should pull image via form', async ({ page }) => {
		test.setTimeout(180_000); // Pulling images can take a long time on CI

		await navigateToImages(page);

		await page.getByRole('button', { name: 'Pull Image' }).click();
		const dialogHeading = page.getByRole('heading', { name: 'Pull Image' });
		await expect(dialogHeading).toBeVisible();

		await page
			.getByRole('textbox', { name: 'Image Name *' })
			.fill('public.ecr.aws/docker/library/alpine');
		await page.getByRole('textbox', { name: 'Tag' }).fill('3.20');

		await page.getByRole('button', { name: 'Pull', exact: true }).click();

		await expect(dialogHeading).toBeHidden({ timeout: 120_000 });
	});
});
