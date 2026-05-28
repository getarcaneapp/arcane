import { test, expect, type Page } from '@playwright/test';
import { fetchVolumeCountsWithRetry } from '../utils/fetch.util';
import { VolumeUsageCounts } from 'types/volumes.type';

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
	await page.getByRole('dialog').locator('input[type="text"]').first().fill(volumeName);
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
		page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')
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
		await expect(content.getByRole('option', { name: /In Use\b/i })).toBeVisible();
		await expect(content.getByRole('option', { name: /Unused\b/i })).toBeVisible();
	});

	test('Inspect Volume', async ({ page }) => {
		const volumeName = `e2e-inspect-volume-${Date.now()}`;

		try {
			await createVolumeViaApi(page, volumeName);
			await page.goto(`/volumes/${encodeURIComponent(volumeName)}`);
			await page.waitForLoadState('load');

			await expect(page).toHaveURL(new RegExp(`/volumes/.+`));
			await expect(page.getByRole('heading', { level: 1, name: volumeName })).toBeVisible();
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
		await page.locator('button[data-slot="arcane-button"][data-action="remove"]').click();
		await page.getByRole('button', { name: 'Remove', exact: true }).last().click();

		await expect(
			page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')
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
			await page.goto(`/volumes/${encodeURIComponent(volumeName)}`);
			await page.waitForLoadState('load');

			await expect(page.getByText('Unused').first()).toBeVisible();
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});
});
