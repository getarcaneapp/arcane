import { test, expect, type Page } from '@playwright/test';
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
	test('loads ignored vulnerabilities from a direct tab URL', async ({ page }) => {
		const ignoredResponse = page.waitForResponse((response) => {
			const request = response.request();
			return (
				request.method() === 'GET' &&
				new URL(response.url()).pathname.endsWith('/vulnerabilities/ignored')
			);
		});

		await page.goto('/images/vulnerabilities?tab=ignored');
		const response = await ignoredResponse;
		expect(response.ok()).toBeTruthy();
		await expect(
			page.getByRole('tab', { name: 'Ignored Vulnerabilities', exact: true })
		).toHaveAttribute('data-state', 'active');
	});

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

		const allImages = await fetchAllImagesForUsage(page);
		const derivedInUse = allImages.filter((img) => !!img.inUse).length;
		const derivedUnused = allImages.length - derivedInUse;

		expect(imageCounts.totalImages).toBe(allImages.length);
		expect(imageCounts.imagesInuse).toBe(derivedInUse);
		expect(imageCounts.imagesUnused).toBe(derivedUnused);
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
		await dialog.getByRole('button', { name: 'Remove', exact: true }).click();
		await expect.poll(() => removeRequestCount).toBe(1);

		await expect(
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
		).toBeVisible();
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
