import { test, expect, type Page } from '@playwright/test';
import { fetchNetworksCountsWithRetry } from '../utils/fetch.util';

async function navigateToNetworks(page: Page) {
	await page.goto('/networks');
	await page.waitForLoadState('load');
}

test.beforeEach(async ({ page }) => {
	await navigateToNetworks(page);
});

async function createNetworkViaUI(page: Page, networkName: string) {
	await navigateToNetworks(page);
	await page.getByRole('button', { name: 'Create Network' }).first().click();
	await expect(page.getByRole('dialog')).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Create New Network' })).toBeVisible();
	await page.locator('#network-name').fill(networkName);

	const createRequest = page.waitForResponse(
		(response) => {
			const request = response.request();
			if (request.method() !== 'POST') return false;
			return /\/api\/environments\/[^/]+\/networks$/.test(new URL(response.url()).pathname);
		},
		{ timeout: 15000 }
	);

	await page.getByRole('dialog').getByRole('button', { name: 'Create Network' }).click();
	const createResponse = await createRequest;
	const responseBody = await createResponse.json().catch(() => undefined);
	if (!createResponse.ok()) {
		const responseText = await createResponse.text().catch(() => '');
		throw new Error(
			`Failed to create network ${networkName}: ${createResponse.status()} ${responseText}`
		);
	}

	return responseBody?.data?.id ?? networkName;
}

async function createNetworkViaApi(page: Page, networkName: string) {
	const response = await page.request.post('/api/environments/0/networks', {
		data: {
			name: networkName,
			options: {
				driver: 'bridge'
			}
		}
	});
	if (!response.ok()) {
		throw new Error(
			`Failed to create network ${networkName}: ${response.status()} ${await response.text()}`
		);
	}
}

async function findNetworkRow(page: Page, networkName: string, maxRetries = 10) {
	for (let i = 0; i < maxRetries; i++) {
		const searchInput = page.getByPlaceholder(/Search/i).first();
		if (await searchInput.isVisible().catch(() => false)) {
			await searchInput.fill(networkName);
		}

		const row = page.locator('tbody tr', { has: page.getByText(networkName) }).first();
		if (await row.isVisible().catch(() => false)) return row;
		await page.waitForTimeout(500);
		await navigateToNetworks(page);
	}
	return page.locator('tbody tr', { has: page.getByText(networkName) }).first();
}

async function removeNetworkViaApi(page: Page, networkName: string) {
	await page.request
		.delete(`/api/environments/0/networks/${encodeURIComponent(networkName)}`)
		.catch(() => undefined);
}

test.describe('Networks Page', () => {
	test.describe.configure({ mode: 'serial' });

	test('Page renders with heading and subtitle', async ({ page }) => {
		await navigateToNetworks(page);
		await expect(page.getByRole('heading', { level: 1, name: 'Networks' })).toBeVisible();
		await expect(page.getByText('Manage your Docker networks').first()).toBeVisible();
	});

	test('Stat cards show correct counts', async ({ page }) => {
		await navigateToNetworks(page);

		// Fetch counts directly in the test to ensure we have fresh data
		const counts = await fetchNetworksCountsWithRetry(page);

		await expect(page.getByText(`${counts.total} Total Networks`)).toBeVisible();
		await expect(page.getByText(`${counts.unused} Unused Networks`)).toBeVisible();
	});

	test('Table displays when networks exist, else empty state', async ({ page }) => {
		const networkName = `e2e-table-network-${Date.now()}`;
		await navigateToNetworks(page);
		try {
			await createNetworkViaApi(page, networkName);
			await navigateToNetworks(page);
			await expect(page.locator('table')).toBeVisible();
			await expect(page.getByRole('button', { name: 'Name' })).toBeVisible();
			await expect(await findNetworkRow(page, networkName)).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});

	test('Open Create Network sheet', async ({ page }) => {
		const networkName = `test-network-${Date.now()}`;
		try {
			const networkId = await createNetworkViaUI(page, networkName);
			const response = await page.request.get(
				`/api/environments/0/networks/${encodeURIComponent(networkId)}`
			);
			expect(response.ok()).toBe(true);
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});

	test('Inspect Network from row actions', async ({ page }) => {
		const networkName = `e2e-inspect-network-${Date.now()}`;
		try {
			await createNetworkViaApi(page, networkName);
			await page.goto(`/networks/${encodeURIComponent(networkName)}`);
			await expect(page).toHaveURL(/\/networks\/.+/);
			await expect(page.getByRole('heading', { level: 1, name: networkName })).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});

	test('Remove Network from details page', async ({ page }) => {
		const networkName = `test-remove-network-${Date.now()}`;
		await createNetworkViaApi(page, networkName);
		await page.goto(`/networks/${encodeURIComponent(networkName)}`);
		await expect(page).toHaveURL(/\/networks\/.+/);
		await page.getByRole('button', { name: 'Remove', exact: true }).click();
		await page.getByRole('button', { name: 'Remove', exact: true }).last().click();
		await expect(
			page.locator('li[data-sonner-toast][data-type="success"] div[data-title]')
		).toBeVisible();

		await expect
			.poll(async () => {
				const response = await page.request.get(
					`/api/environments/0/networks/${encodeURIComponent(networkName)}`
				);
				return response.status();
			})
			.toBe(404);
	});

	test('Default networks cannot be removed on details page', async ({ page }) => {
		await navigateToNetworks(page);
		const bridgeRow = page
			.locator('tbody tr', { has: page.getByText('bridge', { exact: true }) })
			.first();
		await expect(bridgeRow).toBeVisible();
		await bridgeRow.locator('a[href*="/networks/"]').first().click();
		await page.waitForLoadState('load');

		const removeBtn = page.getByRole('button', { name: 'Remove' });
		await expect(removeBtn).toBeDisabled();
	});

	test('Details page shows usage badge', async ({ page }) => {
		const networkName = `e2e-badge-network-${Date.now()}`;
		try {
			await createNetworkViaApi(page, networkName);
			await page.goto(`/networks/${encodeURIComponent(networkName)}`);
			await page.waitForLoadState('load');

			await expect(page.getByText('Unused').first()).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});
});
