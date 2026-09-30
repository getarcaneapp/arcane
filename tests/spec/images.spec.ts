import { test, expect, type Page, type Locator } from '../fixtures/test.fixture';
import {
	fetchImageCountsWithRetry,
	fetchImagesWithRetry,
	readApiData,
	readList
} from '../utils/fetch.util';
import {
	ImageSummary,
	ImageUsageCounts,
	type ImageUpdateInfo,
	type ImageUpdateSummary
} from '../types/image.type';
import { openRowActionsMenu } from '../utils/table-actions.util';
import { waitForDialogReady } from '../utils/playwright.util';

test.describe('Management', () => {
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

	async function fetchAllImagesForUsage(page: Page): Promise<Record<string, unknown>[]> {
		const limit = 200;
		let start = 0;
		const all: Record<string, unknown>[] = [];

		while (true) {
			const { data, pagination } = await readList<Record<string, unknown>>(
				await page.request.get(`${ROUTES.apiImages}?start=${start}&limit=${limit}`),
				'List images for usage'
			);
			all.push(...data);
			expect(pagination?.totalItems, 'Image list must include a total').toBeDefined();
			const totalItems = pagination!.totalItems!;
			if (data.length === 0 || all.length >= totalItems) {
				break;
			}

			start += limit;
		}

		return all;
	}

	async function mockPinnedImageFlow(page: Page) {
		const digest = `sha256:${'8c8ff37a'.padEnd(64, '0')}`;
		const alternateDigest = `sha256:${'9d9aa48b'.padEnd(64, '1')}`;
		const repository = 'registry.example.com:5000/team/syncthing';
		const pinnedReference = `${repository}:2.1.3@${digest}`;
		const alternateReference = `${repository}:stable@${alternateDigest}`;
		const imageId = `sha256:${'abc123'.padEnd(64, '0')}`;
		const image = {
			id: imageId,
			repoTags: [],
			repoDigests: [`${repository}@${digest}`],
			pinnedReferences: [pinnedReference, alternateReference],
			created: 1_725_000_000,
			size: 123_456,
			virtualSize: 123_456,
			labels: null,
			inUse: true,
			usedBy: [{ type: 'container', name: 'syncthing', id: 'container-syncthing' }],
			repo: repository,
			tag: '<none>'
		};

		await page.route('**/api/environments/0/images**', async (route) => {
			const request = route.request();
			const url = new URL(request.url());
			if (request.method() !== 'GET') {
				await route.continue();
				return;
			}

			if (url.pathname === ROUTES.apiImages) {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: [image],
						pagination: {
							totalPages: 1,
							totalItems: 1,
							currentPage: 1,
							itemsPerPage: 20,
							grandTotalItems: 1
						}
					})
				});
				return;
			}

			if (url.pathname === `${ROUTES.apiImages}/counts`) {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: { imagesInuse: 1, imagesUnused: 0, totalImages: 1, totalImageSize: image.size }
					})
				});
				return;
			}

			if (decodeURIComponent(url.pathname) === `${ROUTES.apiImages}/${imageId}`) {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: {
							...image,
							created: '2026-09-04T00:00:00Z',
							comment: '',
							author: '',
							config: {},
							architecture: 'amd64',
							os: 'linux',
							graphDriver: { data: null, name: 'overlay2' },
							rootFs: { type: 'layers', layers: [] },
							metadata: { lastTagTime: '' },
							descriptor: { mediaType: '', digest, size: image.size }
						}
					})
				});
				return;
			}

			await route.continue();
		});

		return { imageId, repository, pinnedReference, alternateReference };
	}

	let realImages: ImageSummary[] = [];
	let imageCounts: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};

	test.beforeEach(async ({ page }) => {
		await navigateToImages(page);

		realImages = await fetchImagesWithRetry(page);
		imageCounts = await fetchImageCountsWithRetry(page);
	});

	test.describe('Images Page', () => {
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

		test('should display full pinned references without enabling tag actions', async ({ page }) => {
			const { imageId, repository, pinnedReference, alternateReference } =
				await mockPinnedImageFlow(page);
			await page.reload();

			const row = page.getByRole('row').filter({
				has: page.getByRole('link', { name: repository, exact: true })
			});
			await expect(row.getByText(pinnedReference, { exact: true })).toBeVisible();

			const overflow = row.getByText('+1', { exact: true });
			await overflow.hover();
			await expect(page.getByText(alternateReference, { exact: true })).toBeVisible();

			const menu = await openRowActionsMenu(page, row);
			await expect(menu.getByRole('menuitem', { name: 'Pull', exact: true })).toBeDisabled();
			await expect(menu.getByRole('menuitem', { name: 'Patch', exact: true })).toBeDisabled();
			await page.keyboard.press('Escape');

			await row.getByRole('link', { name: repository, exact: true }).click();
			await expect
				.poll(() => decodeURIComponent(new URL(page.url()).pathname))
				.toBe(`/images/${imageId}`);
			await expect(page.getByRole('heading', { name: pinnedReference, exact: true })).toBeVisible();
			await expect(page.getByText('Pinned References', { exact: true })).toBeVisible();
			await expect(page.getByText(pinnedReference, { exact: true }).last()).toBeVisible();
			await expect(page.getByText(alternateReference, { exact: true }).last()).toBeVisible();
			await expect(page.getByRole('button', { name: 'Patch', exact: true })).toHaveCount(0);
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
			test.setTimeout(90_000);
			const reference = 'public.ecr.aws/docker/library/busybox:1.37';
			const fixtures = await readList<{ id: string }>(
				await page.request.get(ROUTES.apiImages, { params: { search: reference } }),
				'Find image pull fixture'
			);
			expect(fixtures.data).toHaveLength(1);
			await navigateToImages(page);
			await page.getByPlaceholder('Search…').first().fill(fixtures.data[0].id);

			const row = page
				.getByRole('row')
				.filter({ has: page.locator(`a[href="/images/${fixtures.data[0].id}"]`) });
			const menu = await openRowActionsMenu(page, row);
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === `${ROUTES.apiImages}/pull`
			);
			await menu.getByRole('menuitem', { name: 'Pull' }).click();
			const response = await responsePromise;
			expect(response.ok(), 'Pull fixture image').toBe(true);
			const frames = (await response.text())
				.trim()
				.split('\n')
				.map(
					(line) =>
						JSON.parse(line) as {
							activityId?: string;
							done?: boolean;
							error?: unknown;
						}
				);
			expect(frames.some((frame) => frame.error)).toBe(false);
			expect(frames).toContainEqual(expect.objectContaining({ done: true }));
			const activityId = frames.find((frame) => frame.activityId)?.activityId;
			expect(activityId).toBeTruthy();
			await expect(
				page
					.getByRole('region', { name: 'Notifications alt+T', exact: true })
					.getByRole('listitem')
					.filter({ hasText: `Image ${reference} pulled successfully` })
			).toBeVisible();
			await expect
				.poll(
					async () => {
						const detail = await readApiData<{ activity: { status: string } }>(
							await page.request.get(`/api/environments/0/activities/${activityId}`),
							'Get image pull activity'
						);
						return detail.activity.status;
					},
					{ timeout: 60_000 }
				)
				.toBe('success');
		});

		test('should call remove API on row action remove click and confirmation', async ({ page }) => {
			expect(realImages.length, 'No images available for remove API test').toBeGreaterThan(0);
			await navigateToImages(page);

			const removableImage = realImages.find((image) => image.repo && image.repo !== '<none>');
			if (!removableImage) throw new Error('No removable images available');
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

			// The API list and the UI use different sorting, so the selected image may be on another page.
			const search = page.getByRole('textbox', { name: 'Search…', exact: true });
			await search.fill(removableImage.id);
			await search.press('Enter');

			const imageRow = getImageRows(page).filter({
				has: page.locator(`a[href="/images/${removableImage.id}"]`)
			});
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
			expect(
				realImages.length,
				'At least two images are required for bulk remove test'
			).toBeGreaterThanOrEqual(2);
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
			expect(
				realImages.length,
				'At least two images are required for bulk remove test'
			).toBeGreaterThanOrEqual(2);
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
			await expect(
				pruneButton
					.or(page.getByRole('button', { name: 'More actions' }))
					.filter({ visible: true })
					.first()
			).toBeVisible();
			const isDirectlyVisible = await pruneButton.isVisible();

			if (!isDirectlyVisible) {
				await page.getByRole('button', { name: 'More actions' }).click();
				pruneButton = page.getByRole('menuitem', { name: 'Prune Unused' });
			}

			await pruneButton.click();

			const dialog = page.getByRole('dialog');
			await expect(
				dialog.getByRole('heading', { name: 'Prune Unused Images', exact: true })
			).toBeVisible();
			const pruneResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/environments/0/images/prune'
			);
			await dialog.getByRole('button', { name: 'Prune Images', exact: true }).click();
			await readApiData<Record<string, unknown>>(
				await pruneResponsePromise,
				'Prune dangling images'
			);

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
			await waitForDialogReady(page.getByRole('dialog'));

			await page
				.getByRole('textbox', { name: 'Image Name *' })
				.fill('public.ecr.aws/docker/library/alpine');
			await page.getByRole('textbox', { name: 'Tag' }).fill('3.20');
			await expect(page.getByRole('textbox', { name: 'Image Name *' })).toHaveValue(
				'public.ecr.aws/docker/library/alpine'
			);
			await expect(page.getByRole('textbox', { name: 'Tag' })).toHaveValue('3.20');

			await page.getByRole('button', { name: 'Pull', exact: true }).click();

			await expect(dialogHeading).toBeHidden({ timeout: 120_000 });
		});
	});
});

test.describe('Update checks', () => {
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
		const total = result.pagination?.totalItems;
		if (total === undefined) throw new Error('Image list must include a total');
		return total;
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

	let realImages: ImageSummary[] = [];

	test.beforeEach(async ({ page }) => {
		await navigateToImages(page);

		realImages = await fetchImagesWithRetry(page);
	});

	test.describe('Image Update UI - Check All Updates Button', () => {
		test('should trigger bulk update check when clicking Check Updates button', async ({
			page
		}) => {
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
		test('should show hover card tooltip when hovering over update status icon', async ({
			page
		}) => {
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
			if (!testImage) throw new Error('BusyBox fixture must be present');
			await navigateToImages(page);
			const filteredImagesResponse = page.waitForResponse((response) => {
				const url = new URL(response.url());
				return (
					response.request().method() === 'GET' &&
					url.pathname === '/api/environments/0/images' &&
					url.searchParams.get('search') === testImage.id
				);
			});
			await page.getByPlaceholder('Search…').first().fill(testImage.id);
			const filteredResponse = await filteredImagesResponse;
			expect(filteredResponse.ok()).toBe(true);
			await filteredResponse.finished();
			await expect(page.getByRole('table').locator('tbody tr')).toHaveCount(1);
			const row = page
				.getByRole('row')
				.filter({ has: page.locator(`a[href="/images/${testImage.id}"]`) });
			const trigger = row.getByTestId('image-update-trigger');
			await expect(trigger).toBeVisible();
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					decodeURIComponent(new URL(response.url()).pathname) ===
						`/api/environments/0/image-updates/check/${testImage.id}`
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
		test('should get update summary via API', async ({ page }) => {
			const res = await page.request.get(ROUTES.apiImageUpdatesSummary);

			expect(res.status()).toBe(200);

			const json = await readApiData<ImageUpdateSummary>(res, 'Get image update summary');

			const [imagesTotal, hasUpdateTotal] = await Promise.all([
				fetchImagesTotal(page),
				fetchImagesTotal(page, 'has_update')
			]);

			expect(json.totalImages).toBe(imagesTotal);
			expect(json.imagesWithUpdates).toBe(hasUpdateTotal);
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

			const json = await readApiData<Record<string, ImageUpdateInfo>>(res, 'Check image updates');

			expect(Object.keys(json).length).toBe(0);
		});

		test('should return results for each image in batch', async ({ page }) => {
			const imageRefs = [TEST_IMAGE_REFS.nginx, TEST_IMAGE_REFS.alpine, TEST_IMAGE_REFS.busybox];

			const res = await page.request.post(ROUTES.apiImageUpdatesCheckBatch, {
				data: {
					imageRefs
				}
			});

			expect(res.status()).toBe(200);

			const json = await readApiData<Record<string, ImageUpdateInfo>>(res, 'Check image updates');

			// Each requested image should have a result
			for (const ref of imageRefs) {
				expect(json[ref]).toBeDefined();
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

			const json = await readApiData<Record<string, ImageUpdateInfo>>(res, 'Check image updates');

			// The Public ECR image should succeed.
			expect(json[TEST_IMAGE_REFS.nginx]).toBeDefined();

			// Invalid image should have an error
			const invalidResult = json['invalid-registry.example.com/nonexistent:latest'];
			expect(invalidResult.error).toBeTruthy();
		});
	});
});
