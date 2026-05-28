import { test, expect, type Page } from '@playwright/test';

function extractActivityId(value: unknown): string | undefined {
	if (!value || typeof value !== 'object') return undefined;

	const activityId = (value as { activityId?: unknown }).activityId;
	if (typeof activityId === 'string' && activityId.trim()) return activityId;

	if (Array.isArray(value)) {
		for (const item of value) {
			const nested = extractActivityId(item);
			if (nested) return nested;
		}
		return undefined;
	}

	for (const item of Object.values(value)) {
		const nested = extractActivityId(item);
		if (nested) return nested;
	}

	return undefined;
}

function extractCreatedNetworkId(value: unknown): string | undefined {
	if (!value || typeof value !== 'object') return undefined;
	const data = (value as { data?: { id?: unknown } }).data;
	return typeof data?.id === 'string' ? data.id : undefined;
}

async function createNetworkViaUI(page: Page, networkName: string) {
	await page.goto('/networks');
	await page.waitForLoadState('load');
	await expect(page.getByRole('heading', { level: 1, name: 'Networks' })).toBeVisible();

	await page.getByRole('button', { name: 'Create Network' }).first().click();
	const dialog = page.getByRole('dialog');
	await expect(dialog).toBeVisible();
	await dialog.locator('#network-name').fill(networkName);

	const createRequest = page.waitForResponse(
		(response) => {
			const request = response.request();
			return (
				request.method() === 'POST' &&
				/\/api\/environments\/[^/]+\/networks$/.test(new URL(response.url()).pathname)
			);
		},
		{ timeout: 15000 }
	);

	await dialog.getByRole('button', { name: 'Create Network' }).click();
	const createResponse = await createRequest;
	const body = await createResponse.json();
	if (!createResponse.ok()) {
		throw new Error(`Failed to create network ${networkName}: ${createResponse.status()}`);
	}

	return {
		activityId: extractActivityId(body),
		networkId: extractCreatedNetworkId(body)
	};
}

async function removeNetworkViaApi(page: Page, networkId: string | undefined) {
	if (!networkId) return;
	await page.request
		.delete(`/api/environments/0/networks/${encodeURIComponent(networkId)}`)
		.catch(() => undefined);
}

async function openActivityCenter(page: Page) {
	await page.getByRole('button', { name: 'Open activity center' }).first().click();
	const activityCenter = page.getByRole('dialog', { name: 'Activity Center' });
	await expect(activityCenter).toBeVisible();
	return activityCenter;
}

test.describe('Activity Center', () => {
	test('shows completed activity details for UI-triggered work', async ({ page }) => {
		const networkName = `e2e-activity-network-${Date.now()}`;
		let networkId: string | undefined;

		try {
			const created = await createNetworkViaUI(page, networkName);
			networkId = created.networkId;
			expect(created.activityId).toBeTruthy();

			const activityCenter = await openActivityCenter(page);
			await expect(activityCenter.getByRole('button', { name: 'Running' })).toBeVisible();
			await expect(activityCenter.getByRole('button', { name: 'Failed' })).toBeVisible();
			await activityCenter.getByRole('button', { name: 'Completed' }).click();

			const activityItem = activityCenter
				.locator('button[aria-label="Activity Center"]')
				.filter({ hasText: networkName })
				.first();
			await expect(activityItem).toBeVisible();
			await expect(activityItem).toContainText('Resource Action');
			await expect(activityItem).toContainText('Success');
			await expect(activityItem).toContainText('Local');
			await expect(activityItem).toContainText(/Started by/i);

			await activityItem.click();
			await expect(activityCenter.getByText('Output', { exact: true })).toBeVisible();
			await expect(activityCenter.getByText('Creating network').first()).toBeVisible();
			await expect(activityCenter.getByText('Network created successfully').first()).toBeVisible();
			await expect(activityCenter.getByText('Source environment')).toBeVisible();
			await expect(activityCenter.getByText('Started by', { exact: true })).toBeVisible();
		} finally {
			await removeNetworkViaApi(page, networkId);
		}
	});
});
