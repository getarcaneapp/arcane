import { test, expect, type Locator, type Page } from '../fixtures/test.fixture';
import { fetchNetworksCountsWithRetry } from '../utils/fetch.util';
import authUtil from '../utils/auth.util';

async function navigateToNetworks(page: Page) {
	await page.goto('/networks');
	await page.waitForLoadState('load');
	await expect(page.getByRole('heading', { level: 1, name: 'Networks' })).toBeVisible();
}

async function ensureAuthenticated(page: Page) {
	const currentUserResponse = await page.request.get('/api/auth/me');
	if (!currentUserResponse.ok()) {
		await authUtil.login(page);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();
	}
}

test.beforeEach(async ({ page }) => {
	await ensureAuthenticated(page);
	await navigateToNetworks(page);
});

async function openCreateNetworkDialog(page: Page, networkName: string) {
	await page.getByRole('button', { name: 'Create Network' }).first().click();
	const dialog = page.getByRole('dialog');
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('heading', { name: 'Create New Network' })).toBeVisible();
	await dialog.getByLabel('Network Name *').fill(networkName);
	return dialog;
}

function waitForCreateNetworkResponse(page: Page) {
	return page.waitForResponse(
		(response) => {
			const request = response.request();
			if (request.method() !== 'POST') return false;
			return /\/api\/environments\/[^/]+\/networks$/.test(new URL(response.url()).pathname);
		},
		{ timeout: 15000 }
	);
}

async function fillIpam(
	dialog: Locator,
	values: { subnet?: string; ipRange?: string; gateway?: string }
) {
	await dialog.getByRole('button', { name: 'IPAM Configuration' }).click();
	await dialog.getByLabel('Enable IPAM Configuration').check();
	if (values.subnet !== undefined) await dialog.getByLabel('Subnet').fill(values.subnet);
	if (values.ipRange !== undefined) await dialog.getByLabel('IP Range').fill(values.ipRange);
	if (values.gateway !== undefined) await dialog.getByLabel('Gateway').fill(values.gateway);
}

async function createNetworkViaUI(page: Page, networkName: string) {
	const dialog = await openCreateNetworkDialog(page, networkName);
	const createRequest = waitForCreateNetworkResponse(page);

	await dialog.getByRole('button', { name: 'Create Network' }).click();
	const createResponse = await createRequest;
	const responseBody = await createResponse.json().catch(() => undefined);
	if (!createResponse.ok()) {
		const responseText = await createResponse.text().catch(() => '');
		throw new Error(
			`Failed to create network ${networkName}: ${createResponse.status()} ${responseText}`
		);
	}
	await expect(dialog).toBeHidden();
	await expect(page.getByText(networkName, { exact: true }).first()).toBeVisible();

	return responseBody?.data?.id ?? networkName;
}

async function createNetworkViaApi(page: Page, networkName: string) {
	await page.goto('about:blank');
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
		const searchInput = page.getByPlaceholder('Search…').first();
		if (await searchInput.isVisible().catch(() => false)) {
			await searchInput.fill(networkName);
		}

		const row = page
			.getByRole('row')
			.filter({ has: page.getByRole('link', { name: networkName, exact: true }) })
			.first();
		if (await row.isVisible().catch(() => false)) return row;
		await page.waitForTimeout(500);
		await navigateToNetworks(page);
	}
	return page
		.getByRole('row')
		.filter({ has: page.getByRole('link', { name: networkName, exact: true }) })
		.first();
}

async function removeNetworkViaApi(page: Page, networkName: string) {
	await page.goto('about:blank');
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
			await expect(page.getByRole('table')).toBeVisible();
			await expect(page.getByRole('button', { name: 'Name' })).toBeVisible();
			await expect(await findNetworkRow(page, networkName)).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});

	test('@cross-browser creates and removes a network through the UI', async ({ page }) => {
		const networkName = `test-network-${Date.now()}`;
		try {
			const networkId = await createNetworkViaUI(page, networkName);
			const response = await page.request.get(
				`/api/environments/0/networks/${encodeURIComponent(networkId)}`
			);
			expect(response.ok()).toBe(true);

			await page.goto(`/networks/${encodeURIComponent(networkId)}`);
			await expect(page.getByRole('heading', { name: networkName, level: 1 })).toBeVisible();
			await page.getByRole('button', { name: 'Remove', exact: true }).click();
			await page.getByRole('dialog').getByRole('button', { name: 'Remove', exact: true }).click();
			await expect(page).toHaveURL('/networks');
			await expect(page.getByRole('heading', { name: 'Networks', level: 1 })).toBeVisible();
			await expect
				.poll(async () => {
					const removed = await page.request.get(
						`/api/environments/0/networks/${encodeURIComponent(networkId)}`
					);
					return removed.status();
				})
				.toBe(404);
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
			page.getByRole('region', { name: 'Notifications alt+T', exact: true }).getByRole('listitem')
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
		const bridgeLink = page.getByRole('link', { name: 'bridge', exact: true });
		const bridgeRow = page.getByRole('row').filter({ has: bridgeLink }).first();
		await expect(bridgeRow).toBeVisible();
		await bridgeLink.click();
		await page.waitForLoadState('load');

		const removeBtn = page.getByRole('button', { name: 'Remove' });
		await expect(removeBtn).toBeDisabled();
	});

	test('creates a network with a subnet, IP range, and gateway', async ({ page }) => {
		const networkName = `e2e-iprange-network-${Date.now()}`;
		const subnet = '172.28.0.0/16';
		const ipRange = '172.28.5.0/24';
		const gateway = '172.28.0.1';
		try {
			await openCreateNetworkDialog(page, networkName);
			const dialog = page.getByRole('dialog');
			await fillIpam(dialog, { subnet, ipRange, gateway });

			const createRequest = waitForCreateNetworkResponse(page);
			await dialog.getByRole('button', { name: 'Create Network' }).click();
			const createResponse = await createRequest;
			expect(createResponse.ok()).toBe(true);
			expect(createResponse.request().postDataJSON()).toMatchObject({
				name: networkName,
				options: { ipam: { driver: 'default', config: [{ subnet, ipRange, gateway }] } }
			});
			await expect(dialog).toBeHidden();

			const inspect = await page.request.get(
				`/api/environments/0/networks/${encodeURIComponent(networkName)}`
			);
			expect(inspect.ok()).toBe(true);
			const inspected = await inspect.json();
			expect(inspected.data.ipam.config[0]).toMatchObject({ subnet, ipRange, gateway });

			await page.goto(`/networks/${encodeURIComponent(networkName)}`);
			await expect(page.getByRole('heading', { level: 1, name: networkName })).toBeVisible();
			await expect(page.getByText(ipRange, { exact: true })).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
	});

	test('omits blank IP range, drops IPAM when disabled, and resets on close', async ({ page }) => {
		const blankRangeName = `e2e-blank-range-${Date.now()}`;
		const disabledIpamName = `e2e-ipam-off-${Date.now()}`;
		try {
			await openCreateNetworkDialog(page, blankRangeName);
			const dialog = page.getByRole('dialog');
			await fillIpam(dialog, { subnet: '172.29.0.0/16', ipRange: '   ' });

			let createRequest = waitForCreateNetworkResponse(page);
			await dialog.getByRole('button', { name: 'Create Network' }).click();
			let createResponse = await createRequest;
			expect(createResponse.ok()).toBe(true);
			const blankBody = createResponse.request().postDataJSON();
			expect(blankBody.options.ipam.config[0]).toEqual({ subnet: '172.29.0.0/16' });
			await expect(dialog).toBeHidden();

			await openCreateNetworkDialog(page, disabledIpamName);
			await fillIpam(dialog, { subnet: '172.30.0.0/16', ipRange: '172.30.1.0/24' });
			await dialog.getByLabel('Enable IPAM Configuration').uncheck();
			await expect(dialog.getByLabel('IP Range')).toBeHidden();

			createRequest = waitForCreateNetworkResponse(page);
			await dialog.getByRole('button', { name: 'Create Network' }).click();
			createResponse = await createRequest;
			expect(createResponse.ok()).toBe(true);
			expect(createResponse.request().postDataJSON().options.ipam).toBeUndefined();
			await expect(dialog).toBeHidden();

			await openCreateNetworkDialog(page, 'e2e-reset-check');
			await fillIpam(dialog, { ipRange: '172.31.1.0/24' });
			await dialog.getByRole('button', { name: 'Cancel' }).click();
			await expect(dialog).toBeHidden();

			await page.getByRole('button', { name: 'Create Network' }).first().click();
			await expect(dialog).toBeVisible();
			await expect(dialog.getByLabel('Enable IPAM Configuration')).toBeHidden();
			await dialog.getByRole('button', { name: 'IPAM Configuration' }).click();
			await expect(dialog.getByLabel('Enable IPAM Configuration')).not.toBeChecked();
			await dialog.getByLabel('Enable IPAM Configuration').check();
			await expect(dialog.getByLabel('IP Range')).toHaveValue('');
		} finally {
			await removeNetworkViaApi(page, blankRangeName);
			await removeNetworkViaApi(page, disabledIpamName);
		}
	});

	test('shows an actionable error for an invalid or out-of-subnet IP range', async ({ page }) => {
		const networkName = `e2e-bad-cidr-${Date.now()}`;
		const toast = page
			.getByRole('region', { name: 'Notifications alt+T', exact: true })
			.getByRole('listitem');
		try {
			await openCreateNetworkDialog(page, networkName);
			const dialog = page.getByRole('dialog');
			await fillIpam(dialog, { subnet: '172.32.0.0/16', ipRange: 'not-a-cidr' });

			let createRequest = waitForCreateNetworkResponse(page);
			await dialog.getByRole('button', { name: 'Create Network' }).click();
			expect((await createRequest).ok()).toBe(false);
			await expect(toast.filter({ hasText: 'not-a-cidr' })).toContainText('Failed to create');
			await expect(dialog).toBeVisible();
			await expect(dialog.getByLabel('IP Range')).toHaveValue('not-a-cidr');

			await dialog.getByLabel('IP Range').fill('10.99.0.0/24');
			createRequest = waitForCreateNetworkResponse(page);
			await dialog.getByRole('button', { name: 'Create Network' }).click();
			expect((await createRequest).ok()).toBe(false);
			await expect(toast.filter({ hasText: "doesn't contain ip-range" })).toContainText(
				'10.99.0.0/24'
			);
			await expect(dialog).toBeVisible();
			await expect(dialog.getByLabel('Network Name *')).toHaveValue(networkName);
		} finally {
			await removeNetworkViaApi(page, networkName);
		}
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
