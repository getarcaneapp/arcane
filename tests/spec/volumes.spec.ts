import { test, expect, type Page } from '@playwright/test';
import { fetchVolumeCountsWithRetry } from '../utils/fetch.util';
import { VolumeUsageCounts } from 'types/volumes.type';

let volumeCount: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };
const VOLUMES_PREFERENCE_KEY = 'arcane-volumes-table';

async function setVolumesPreference(page: Page, prefs: Record<string, unknown>) {
	const response = await page.request.patch('/api/auth/me/prefs', {
		data: { tables: { [VOLUMES_PREFERENCE_KEY]: prefs } }
	});
	if (!response.ok()) {
		throw new Error(
			`Failed to seed volume preferences: ${response.status()} ${await response.text()}`
		);
	}
}

async function getPersistedVolumesSort(page: Page): Promise<string> {
	const response = await page.request.get('/api/auth/me/prefs');
	if (!response.ok()) return '';
	const body = await response.json();
	const sort = body?.data?.tables?.[VOLUMES_PREFERENCE_KEY]?.s;
	return Array.isArray(sort) ? sort.join(':') : '';
}

test.beforeEach(async ({ page }) => {
	const response = await page.request.patch('/api/auth/me/prefs', {
		data: { tables: { [VOLUMES_PREFERENCE_KEY]: null } }
	});
	if (!response.ok()) {
		throw new Error(
			`Failed to clear volume preferences: ${response.status()} ${await response.text()}`
		);
	}
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
		await setVolumesPreference(page, { v: [], f: [], g: '', l: 20, s: ['size', 'desc'] });

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

		await expect.poll(() => getPersistedVolumesSort(page)).toBe('size:desc');
	});

	test('Hidden Size column skips usage loading and resets its sort', async ({ page, context }) => {
		await setVolumesPreference(page, { v: ['size'], f: [], g: '', l: 20, s: ['size', 'desc'] });

		let sizeRequests = 0;
		await context.route('**/api/environments/*/volumes/sizes', async (route) => {
			sizeRequests += 1;
			await route.continue();
		});

		await page.goto('/volumes');
		await expect(page.getByRole('heading', { name: 'Volumes', level: 1 })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Size', exact: true })).toHaveCount(0);
		await expect.poll(() => getPersistedVolumesSort(page)).toBe('name:asc');
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
