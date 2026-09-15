import { test, expect, type Page } from '../fixtures/test.fixture';
import { fetchVolumeCountsWithRetry } from '../utils/fetch.util';
import { VolumeUsageCounts } from 'types/volumes.type';
import { openRowActionsMenu } from '../utils/table-actions.util';

let volumeCount: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

test.beforeEach(async ({ page }) => {
	await page.goto('/volumes');
	volumeCount = await fetchVolumeCountsWithRetry(page);
});

async function openCreateVolumeSheet(page: Page) {
	await page.goto('/volumes');
	await page.waitForLoadState('load');
	await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible();

	const createButton = page.getByRole('button', { name: 'Create Volume' }).first();
	if (await createButton.isVisible().catch(() => false)) {
		await createButton.click();
	} else {
		const overflowButton = page.getByRole('button', { name: 'More actions' }).first();
		await expect(overflowButton).toBeVisible();
		await overflowButton.click();
		await page.getByRole('menuitem', { name: 'Create Volume', exact: true }).click();
	}

	await expect(page.getByRole('dialog')).toBeVisible();
}

async function createVolumeViaUI(page: Page, volumeName: string) {
	await openCreateVolumeSheet(page);
	await page.getByRole('dialog').getByLabel('Volume Name *').fill(volumeName);
	const createRequest = page.waitForResponse(
		(response) => {
			const request = response.request();
			return (
				request.method() === 'POST' &&
				/\/api\/environments\/[^/]+\/volumes$/.test(new URL(response.url()).pathname)
			);
		},
		{ timeout: 15000 }
	);
	await page.getByRole('dialog').getByRole('button', { name: 'Create Volume' }).click();
	const createResponse = await createRequest;
	if (!createResponse.ok()) {
		throw new Error(
			`Failed to create volume ${volumeName}: ${createResponse.status()} ${await createResponse.text()}`
		);
	}
	await expect(
		page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
	).toBeVisible();
}

async function createVolumeViaApi(page: Page, volumeName: string) {
	const response = await page.request.post('/api/environments/0/volumes', {
		data: {
			name: volumeName,
			driver: 'local'
		}
	});
	if (!response.ok()) {
		throw new Error(
			`Failed to create volume ${volumeName}: ${response.status()} ${await response.text()}`
		);
	}
}

async function removeVolumeViaApi(page: Page, volumeName: string) {
	await page.request
		.delete(`/api/environments/0/volumes/${encodeURIComponent(volumeName)}`)
		.catch(() => undefined);
}

async function gotoVolumeDetail(page: Page, volumeName: string) {
	const volumePath = `/api/environments/0/volumes/${encodeURIComponent(volumeName)}`;
	const detailResponse = page.waitForResponse((response) => {
		const request = response.request();
		return request.method() === 'GET' && new URL(response.url()).pathname === volumePath;
	});

	await page.goto(`/volumes/${encodeURIComponent(volumeName)}`);
	const response = await detailResponse;
	expect(response.ok(), `Expected successful GET ${volumePath}`).toBeTruthy();
	await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
	await expect(page.getByRole('heading', { level: 1, name: volumeName })).toBeVisible();
}

function facetIds(title: string) {
	const key = title.toLowerCase();
	return {
		triggerId: `facet-${key}-trigger`,
		contentId: `facet-${key}-content`
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
	test('Persisted Size sort does not block navigation', async ({ page, context }) => {
		await page.goto('/dashboard');
		await page.evaluate(() => {
			localStorage.setItem(
				'arcane-volumes-table',
				JSON.stringify({ v: [], f: [], g: '', l: 20, s: ['size', 'desc'] })
			);
		});

		let releaseSizeSort: () => void = () => undefined;
		const sizeSortGate = new Promise<void>((resolve) => {
			releaseSizeSort = resolve;
		});
		let sizeSortRequests = 0;
		await context.route('**/api/environments/*/volumes**', async (route) => {
			const request = route.request();
			const url = new URL(request.url());
			if (
				request.method() === 'GET' &&
				/^\/api\/environments\/[^/]+\/volumes$/.test(url.pathname) &&
				url.searchParams.get('sort') === 'size'
			) {
				sizeSortRequests += 1;
				await sizeSortGate;
			}
			await route.continue();
		});

		try {
			await page.goto('/volumes');
			await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible({
				timeout: 2000
			});
			await expect.poll(() => sizeSortRequests).toBeGreaterThan(0);
		} finally {
			releaseSizeSort();
		}

		await expect
			.poll(() =>
				page.evaluate(() => {
					const stored = localStorage.getItem('arcane-volumes-table');
					const sort = stored ? JSON.parse(stored).s : undefined;
					return Array.isArray(sort) ? sort.join(':') : '';
				})
			)
			.toBe('size:desc');
	});

	test('Hidden Size column skips usage loading and resets its sort', async ({ page, context }) => {
		await page.goto('/dashboard');
		await page.evaluate(() => {
			localStorage.setItem(
				'arcane-volumes-table',
				JSON.stringify({ v: ['size'], f: [], g: '', l: 20, s: ['size', 'desc'] })
			);
		});

		let sizeRequests = 0;
		await context.route('**/api/environments/*/volumes/sizes', async (route) => {
			sizeRequests += 1;
			await route.continue();
		});

		await page.goto('/volumes');
		await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Size', exact: true })).toHaveCount(0);
		await expect
			.poll(() =>
				page.evaluate(() => {
					const stored = localStorage.getItem('arcane-volumes-table');
					const sort = stored ? JSON.parse(stored).s : undefined;
					return Array.isArray(sort) ? sort.join(':') : '';
				})
			)
			.toBe('name:asc');
		expect(sizeRequests).toBe(0);
	});

	test('Volume Page Display', async ({ page }) => {
		await page.goto('/volumes');

		await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible();
		await expect(page.getByText('Manage your Docker volumes').first()).toBeVisible();
	});

	test('Correct Volume Stat Card Counts', async ({ page }) => {
		await page.goto('/volumes');
		await page.waitForLoadState('load');

		await expect(page.getByText(`${volumeCount.total} Total Volumes`)).toBeVisible();
	});

	test('Create Volume Sheet Opens', async ({ page }) => {
		await openCreateVolumeSheet(page);
		await expect(page.getByText('Create New Volume')).toBeVisible();
	});

	test('Display Volume Filters', async ({ page }) => {
		await page.goto('/volumes');
		await page.waitForLoadState('load');

		const { content } = await ensureFacetOpen(page, 'Usage');
		await expect(content.getByRole('option', { name: 'In Use' })).toBeVisible();
		await expect(content.getByRole('option', { name: 'Unused' })).toBeVisible();
	});

	test('Inspect Volume', async ({ page }) => {
		const volumeName = `e2e-inspect-volume-${Date.now()}`;

		try {
			await createVolumeViaApi(page, volumeName);
			await gotoVolumeDetail(page, volumeName);
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});

	test('Remove Volume', async ({ page }) => {
		const volumeName = `test-remove-volume-${Date.now()}`;
		await createVolumeViaApi(page, volumeName);
		await page.goto(`/volumes/${encodeURIComponent(volumeName)}`);
		await page.waitForLoadState('load');

		await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
		await page.getByRole('button', { name: 'Remove', exact: true }).click();
		await page.getByRole('dialog').getByRole('button', { name: 'Remove', exact: true }).click();

		await expect(
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
		).toBeVisible();
	});

	test('Create Volume', async ({ page }) => {
		const volumeName = `test-volume-${Date.now()}`;
		try {
			await createVolumeViaUI(page, volumeName);
			const response = await page.request.get(
				`/api/environments/0/volumes/${encodeURIComponent(volumeName)}`
			);
			expect(response.ok()).toBe(true);
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});

	test('Rename unused volume', async ({ page }) => {
		const sourceName = `test-rename-source-${Date.now()}`;
		const targetName = sourceName.replace('source', 'target');

		try {
			await createVolumeViaApi(page, sourceName);
			await page.goto('/volumes');
			const search = page.getByPlaceholder('Search…');
			await search.fill(sourceName);

			const row = page
				.getByRole('row')
				.filter({ has: page.getByRole('link', { name: sourceName, exact: true }) });
			const menu = await openRowActionsMenu(page, row);
			await menu.getByRole('menuitem', { name: 'Rename', exact: true }).click();

			const dialog = page.getByRole('dialog');
			await expect(dialog).toBeVisible();
			await dialog.getByLabel('New volume name').fill(targetName);

			const renameRequest = page.waitForResponse((response) => {
				const request = response.request();
				return (
					request.method() === 'POST' &&
					new URL(response.url()).pathname.endsWith(
						`/volumes/${encodeURIComponent(sourceName)}/rename`
					)
				);
			});
			await dialog.getByRole('button', { name: 'Rename', exact: true }).click();
			const response = await renameRequest;
			expect(response.ok(), await response.text()).toBe(true);

			await search.fill(targetName);
			await expect(page.getByRole('link', { name: targetName, exact: true })).toBeVisible();
			expect(
				(
					await page.request.get(`/api/environments/0/volumes/${encodeURIComponent(sourceName)}`)
				).status()
			).toBe(404);
			expect(
				(
					await page.request.get(`/api/environments/0/volumes/${encodeURIComponent(targetName)}`)
				).ok()
			).toBe(true);
		} finally {
			await removeVolumeViaApi(page, sourceName);
			await removeVolumeViaApi(page, targetName);
		}
	});

	test('Display correct volume usage badge', async ({ page }) => {
		const volumeName = `e2e-badge-volume-${Date.now()}`;
		try {
			await createVolumeViaApi(page, volumeName);
			await gotoVolumeDetail(page, volumeName);

			await expect(page.getByText('Unused').first()).toBeVisible();
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});
});

type MockVolume = {
	id: string;
	name: string;
	driver: string;
	mountpoint: string;
	scope: string;
	options: null;
	labels: Record<string, string>;
	createdAt: string;
	inUse: boolean;
	size: number;
};

function mockVolume(index: number): MockVolume {
	const name = `shift-vol-${String(index).padStart(3, '0')}`;
	return {
		id: name,
		name,
		driver: 'local',
		mountpoint: `/var/lib/docker/volumes/${name}/_data`,
		scope: 'local',
		options: null,
		labels: {},
		createdAt: new Date(1_700_000_000_000 + index * 60_000).toISOString(),
		inUse: index % 2 === 0,
		size: index
	};
}

function volumesPage(volumes: MockVolume[], url: URL) {
	const search = (url.searchParams.get('search') ?? '').toLowerCase();
	const sort = url.searchParams.get('sort') ?? 'name';
	const order = url.searchParams.get('order') ?? 'asc';
	const start = Number(url.searchParams.get('start') ?? '0');
	const limit = Number(url.searchParams.get('limit') ?? '20');
	const filtered = volumes.filter((volume) => volume.name.toLowerCase().includes(search));
	const sorted = [...filtered].sort((a, b) => {
		const av = a[sort as keyof MockVolume];
		const bv = b[sort as keyof MockVolume];
		const cmp =
			typeof av === 'number' && typeof bv === 'number'
				? av - bv
				: String(av).localeCompare(String(bv));
		return order === 'desc' ? -cmp : cmp;
	});
	const data = limit === -1 ? sorted : sorted.slice(start, start + limit);
	const inuse = volumes.filter((volume) => volume.inUse).length;
	return {
		success: true,
		data,
		counts: { inuse, unused: volumes.length - inuse, total: volumes.length },
		pagination: {
			totalPages: limit === -1 ? 1 : Math.max(1, Math.ceil(sorted.length / limit)),
			totalItems: sorted.length,
			currentPage: limit === -1 ? 1 : Math.floor(start / limit) + 1,
			itemsPerPage: limit === -1 ? sorted.length : limit,
			grandTotalItems: volumes.length
		}
	};
}

async function mockVolumeList(page: Page, volumesByEnv: Record<string, MockVolume[]>) {
	await page.context().route(/\/api\/environments\/[^/]+\/volumes(?:\?.*)?$/, async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const envId = url.pathname.split('/')[3];
		const volumes = volumesByEnv[envId];
		if (request.method() !== 'GET' || !volumes) {
			await route.continue();
			return;
		}
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(volumesPage(volumes, url))
		});
	});
	await page.context().route(/\/api\/environments\/[^/]+\/volumes\/sizes$/, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ success: true, data: [] })
		});
	});
}

function volumeRow(page: Page, index: number) {
	const name = mockVolume(index).name;
	return page.getByRole('row').filter({ has: page.getByRole('link', { name, exact: true }) });
}

function rowCheckbox(page: Page, index: number) {
	return volumeRow(page, index).getByRole('checkbox');
}

async function expectSelected(page: Page, indexes: number[], selected: boolean) {
	for (const index of indexes) {
		await expect(rowCheckbox(page, index)).toHaveAttribute(
			'aria-checked',
			selected ? 'true' : 'false'
		);
		if (selected) await expect(volumeRow(page, index)).toHaveAttribute('data-state', 'selected');
		else await expect(volumeRow(page, index)).not.toHaveAttribute('data-state', 'selected');
	}
}

function removeSelectedButton(page: Page, count: number) {
	return page.getByRole('button', { name: `Remove Selected (${count})`, exact: true });
}

async function openShiftSelectVolumes(page: Page, count = 12) {
	const volumes = Array.from({ length: count }, (_, index) => mockVolume(index + 1));
	await page.addInitScript(() => {
		localStorage.removeItem('selectedEnvironmentId');
		localStorage.removeItem('arcane-volumes-table');
	});
	await mockVolumeList(page, { '0': volumes });
	await page.goto('/volumes');
	await expect(page.getByRole('link', { name: volumes[0].name, exact: true })).toBeVisible();
	return volumes;
}

test.describe('Volumes shift-select', () => {
	test('shift-click selects the range between anchor and target', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 2).click();
		await rowCheckbox(page, 6).click({ modifiers: ['Shift'] });

		await expectSelected(page, [2, 3, 4, 5, 6], true);
		await expectSelected(page, [1, 7, 8], false);
		await expect(removeSelectedButton(page, 5)).toBeVisible();
	});

	test('reverse range via row clicks keeps selections outside the range', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 1).click();
		await volumeRow(page, 9).getByRole('cell').nth(3).click();
		await volumeRow(page, 5)
			.getByRole('cell')
			.nth(3)
			.click({ modifiers: ['Shift'] });

		await expectSelected(page, [1, 5, 6, 7, 8, 9], true);
		await expectSelected(page, [2, 3, 4, 10], false);
		await expect(removeSelectedButton(page, 6)).toBeVisible();
	});

	test('shift-click deselects a range when the target is being unchecked', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 1).click();
		await rowCheckbox(page, 8).click({ modifiers: ['Shift'] });
		await expect(removeSelectedButton(page, 8)).toBeVisible();

		await rowCheckbox(page, 3).click();
		await rowCheckbox(page, 6).click({ modifiers: ['Shift'] });

		await expectSelected(page, [3, 4, 5, 6], false);
		await expectSelected(page, [1, 2, 7, 8], true);
		await expect(removeSelectedButton(page, 4)).toBeVisible();
	});

	test('repeated shift-clicks keep the original anchor', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 3).click();
		await rowCheckbox(page, 5).click({ modifiers: ['Shift'] });
		await rowCheckbox(page, 8).click({ modifiers: ['Shift'] });

		await expectSelected(page, [3, 4, 5, 6, 7, 8], true);
		await expectSelected(page, [1, 2, 9], false);

		await rowCheckbox(page, 1).click({ modifiers: ['Shift'] });
		await expectSelected(page, [1, 2, 3], true);
		await expect(removeSelectedButton(page, 8)).toBeVisible();
	});

	test('shift-click without an anchor behaves like a normal click', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 4).click({ modifiers: ['Shift'] });

		await expectSelected(page, [4], true);
		await expectSelected(page, [1, 2, 3, 5], false);
		await expect(removeSelectedButton(page, 1)).toBeVisible();
	});

	test('Shift+Space selects a range while Space toggles a single row', async ({ page }) => {
		await openShiftSelectVolumes(page);

		await rowCheckbox(page, 2).click();
		await rowCheckbox(page, 5).focus();
		await page.keyboard.press('Shift+Space');

		await expectSelected(page, [2, 3, 4, 5], true);
		await expectSelected(page, [1, 6], false);

		await rowCheckbox(page, 7).focus();
		await page.keyboard.press('Space');
		await expectSelected(page, [7], true);
		await expectSelected(page, [6], false);
		await expect(removeSelectedButton(page, 5)).toBeVisible();
	});

	test('header selection reflects ranges and resets the anchor', async ({ page }) => {
		await openShiftSelectVolumes(page);
		const headerCheckbox = page.getByRole('checkbox', { name: 'Select all' });

		await rowCheckbox(page, 2).click();
		await rowCheckbox(page, 3).click({ modifiers: ['Shift'] });
		await expect(headerCheckbox).toHaveAttribute('aria-checked', 'mixed');

		await headerCheckbox.click();
		await expect(headerCheckbox).toHaveAttribute('aria-checked', 'true');
		await expect(removeSelectedButton(page, 12)).toBeVisible();

		await headerCheckbox.click();
		await expect(headerCheckbox).toHaveAttribute('aria-checked', 'false');
		await expect(removeSelectedButton(page, 12)).toHaveCount(0);

		await rowCheckbox(page, 6).click({ modifiers: ['Shift'] });
		await expectSelected(page, [6], true);
		await expectSelected(page, [2, 3, 4, 5], false);
	});

	test('page, sort, and search changes reset the anchor', async ({ page }) => {
		await openShiftSelectVolumes(page, 30);

		await rowCheckbox(page, 2).click();
		await page.getByRole('button', { name: 'Go to next page' }).click();
		await expect(page.getByRole('link', { name: mockVolume(21).name, exact: true })).toBeVisible();
		await rowCheckbox(page, 25).click({ modifiers: ['Shift'] });
		await expectSelected(page, [25], true);
		await expectSelected(page, [21, 22, 23, 24], false);
		await expect(removeSelectedButton(page, 2)).toBeVisible();

		await page.getByRole('button', { name: 'Go to first page' }).click();
		await expect(page.getByRole('link', { name: mockVolume(1).name, exact: true })).toBeVisible();
		await rowCheckbox(page, 4).click();
		await page.getByRole('button', { name: 'Name', exact: true }).click();
		await page.getByRole('menuitem', { name: 'Desc' }).click();
		await expect(page.getByRole('link', { name: mockVolume(30).name, exact: true })).toBeVisible();
		await rowCheckbox(page, 12).click({ modifiers: ['Shift'] });
		await expectSelected(page, [12], true);
		await expectSelected(page, [11, 13], false);
		await expect(removeSelectedButton(page, 4)).toBeVisible();

		await page.getByPlaceholder('Search…').fill('shift-vol-01');
		await expect(page.getByRole('link', { name: mockVolume(30).name, exact: true })).toHaveCount(0);
		await rowCheckbox(page, 15).click({ modifiers: ['Shift'] });
		await expectSelected(page, [15], true);
		await expectSelected(page, [13, 14, 16], false);
		await expect(removeSelectedButton(page, 5)).toBeVisible();
	});

	test('switching environments resets the anchor', async ({ page }) => {
		const localEnvironment = {
			id: '0',
			name: 'Local Test',
			apiUrl: 'unix:///var/run/docker.sock',
			status: 'online',
			enabled: true,
			isEdge: false
		};
		const remoteEnvironment = {
			id: 'remote-shift-test',
			name: 'Remote Test',
			apiUrl: 'https://remote.example.invalid',
			status: 'online',
			enabled: true,
			isEdge: false
		};
		const environments = [localEnvironment, remoteEnvironment];
		await page.context().route(/\/api\/environments(?:\?.*)?$/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					success: true,
					data: environments,
					pagination: {
						totalPages: 1,
						totalItems: 2,
						currentPage: 1,
						itemsPerPage: 20,
						grandTotalItems: 2
					}
				})
			});
		});
		await page.context().route(/\/api\/stream(?:\?.*)?$/, async (route) => {
			const channels =
				new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
			const timestamp = new Date().toISOString();
			await route.fulfill({
				status: 200,
				contentType: 'application/x-json-stream',
				body: channels.includes('environments')
					? `${JSON.stringify({ channel: 'environments', environment: { type: 'snapshot', environments, timestamp }, timestamp })}\n`
					: ''
			});
		});
		await page
			.context()
			.route(/\/api\/environments\/remote-shift-test\/settings$/, async (route) => {
				await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
			});
		const remoteVolumes = Array.from({ length: 8 }, (_, index) => mockVolume(index + 101));
		await page.addInitScript(() => {
			localStorage.removeItem('selectedEnvironmentId');
			localStorage.removeItem('arcane-volumes-table');
		});
		await mockVolumeList(page, {
			'0': Array.from({ length: 12 }, (_, index) => mockVolume(index + 1)),
			'remote-shift-test': remoteVolumes
		});
		await page.goto('/volumes');
		await expect(page.getByRole('link', { name: mockVolume(1).name, exact: true })).toBeVisible();

		await rowCheckbox(page, 2).click();
		await page.getByRole('button').filter({ hasText: localEnvironment.name }).first().click();
		const dialog = page.getByRole('dialog', { name: 'Select Environment' });
		await expect(dialog).toBeVisible();
		await dialog.getByRole('button').filter({ hasText: remoteEnvironment.name }).first().click();
		await expect(page.getByRole('link', { name: mockVolume(101).name, exact: true })).toBeVisible();
		await expect(page.getByRole('link', { name: mockVolume(1).name, exact: true })).toHaveCount(0);

		await rowCheckbox(page, 105).click({ modifiers: ['Shift'] });
		await expectSelected(page, [105], true);
		await expectSelected(page, [101, 102, 103, 104], false);
		await expect(removeSelectedButton(page, 1)).toBeVisible();
	});

	test('ranges span a virtualized list larger than 100 rows', async ({ page }) => {
		const volumes = Array.from({ length: 150 }, (_, index) => mockVolume(index + 1));
		await page.addInitScript(() => {
			localStorage.removeItem('selectedEnvironmentId');
			localStorage.setItem('arcane-volumes-table', JSON.stringify({ v: [], f: [], g: '', l: -1 }));
		});
		await mockVolumeList(page, { '0': volumes });
		await page.goto('/volumes');
		await expect(page.getByRole('link', { name: mockVolume(1).name, exact: true })).toBeVisible();
		await expect(page.locator('table tbody tr[data-index]').first()).toBeAttached();

		await rowCheckbox(page, 1).click();
		await rowCheckbox(page, 140).scrollIntoViewIfNeeded();
		await rowCheckbox(page, 140).click({ modifiers: ['Shift'] });

		await expect(removeSelectedButton(page, 140)).toBeVisible();
		await expectSelected(page, [140], true);
		await expectSelected(page, [141, 150], false);

		await rowCheckbox(page, 1).scrollIntoViewIfNeeded();
		await expectSelected(page, [1, 2, 3, 70], true);
	});
});
