import { test, expect, type Locator, type Page, type Route } from '@playwright/test';

type MockEnvironment = {
	id: string;
	name: string;
	apiUrl: string;
	status: 'online' | 'standby' | 'offline' | 'error' | 'pending';
	enabled: boolean;
	isEdge: boolean;
};

type MockActivity = {
	id: string;
	environmentId: string;
	sourceEnvironmentId?: string;
	sourceEnvironmentName?: string;
	type: string;
	status: string;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
	latestMessage?: string;
	startedAt: string;
	createdAt: string;
	updatedAt?: string;
};

const localEnvironment: MockEnvironment = {
	id: '0',
	name: 'Local',
	apiUrl: 'unix:///var/run/docker.sock',
	status: 'online',
	enabled: true,
	isEdge: false
};

const remoteEnvironment: MockEnvironment = {
	id: 'remote-activity-test',
	name: 'Remote Lab',
	apiUrl: 'https://remote.example.invalid',
	status: 'offline',
	enabled: true,
	isEdge: false
};

function paginated<T>(data: T[]) {
	return {
		success: true,
		data,
		pagination: {
			totalPages: 1,
			totalItems: data.length,
			currentPage: 1,
			itemsPerPage: data.length,
			grandTotalItems: data.length
		}
	};
}

function activity(
	id: string,
	environmentId: string,
	sourceEnvironmentName: string,
	resourceName: string,
	minutesAgo: number
): MockActivity {
	const timestamp = new Date(Date.now() - minutesAgo * 60_000).toISOString();
	return {
		id,
		environmentId,
		sourceEnvironmentId: environmentId,
		sourceEnvironmentName,
		type: 'resource_action',
		status: 'success',
		resourceType: 'network',
		resourceId: resourceName,
		resourceName,
		latestMessage: `${resourceName} completed`,
		startedAt: timestamp,
		createdAt: timestamp,
		updatedAt: timestamp
	};
}

function activityEnvironmentIdFromPath(pathname: string): string | null {
	const match = pathname.match(/^\/api\/environments\/([^/]+)\/activities$/);
	return match ? decodeURIComponent(match[1]) : null;
}

async function preserveLocalEnvironmentSelection(page: Page) {
	await page.addInitScript(() => {
		localStorage.removeItem('selectedEnvironmentId');
	});
}

async function mockEnvironmentList(page: Page, environments: MockEnvironment[]) {
	await page.context().route(/\/api\/environments(?:\?.*)?$/, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(paginated(environments))
		});
	});
}

function aggregatedActivityStreamBody(
	activitiesByEnvironment: Record<string, MockActivity[]>,
	failedEnvironmentIds: Set<string>
): string {
	const timestamp = new Date().toISOString();
	const events: string[] = [];
	for (const [environmentId, activities] of Object.entries(activitiesByEnvironment)) {
		events.push(JSON.stringify({ type: 'snapshot', environmentId, activities, timestamp }));
	}
	for (const environmentId of failedEnvironmentIds) {
		events.push(
			JSON.stringify({ type: 'error', environmentId, error: 'environment unavailable', timestamp })
		);
	}
	return events.join('\n') + '\n';
}

async function mockActivityReads(
	page: Page,
	activitiesByEnvironment: Record<string, MockActivity[]>,
	failedEnvironmentIds = new Set<string>(),
	failedActivityStatus = 503
) {
	await page.context().route(/\/api\/activities\/stream(?:\?.*)?$/, async (route: Route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/x-json-stream',
			body: aggregatedActivityStreamBody(activitiesByEnvironment, failedEnvironmentIds)
		});
	});

	await page
		.context()
		.route(/\/api\/environments\/[^/]+\/activities(?:\?.*)?$/, async (route: Route) => {
			const url = new URL(route.request().url());
			const environmentId = activityEnvironmentIdFromPath(url.pathname);
			if (!environmentId) {
				await route.continue();
				return;
			}

			if (failedEnvironmentIds.has(environmentId)) {
				await route.fulfill({
					status: failedActivityStatus,
					contentType: 'application/json',
					body: JSON.stringify({
						success: false,
						message: 'environment unavailable',
						detail: 'permission denied: activities:read'
					})
				});
				return;
			}

			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(paginated(activitiesByEnvironment[environmentId] ?? []))
			});
		});
}

function waitForActivityRead(page: Page, environmentId: string) {
	return page.waitForResponse((response) => {
		const url = new URL(response.url());
		return (
			response.request().method() === 'GET' &&
			activityEnvironmentIdFromPath(url.pathname) === environmentId
		);
	});
}

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

function activityRow(activityCenter: Locator, text: string) {
	return activityCenter
		.locator('button[aria-label="Activity Center"]')
		.filter({ hasText: text })
		.first();
}

function waitForActivityStream(page: Page) {
	return page.waitForResponse((response) => {
		const url = new URL(response.url());
		return response.request().method() === 'GET' && url.pathname === '/api/activities/stream';
	});
}

test.describe('Activity Center', () => {
	test('shows activity from every configured environment', async ({ page }) => {
		await preserveLocalEnvironmentSelection(page);
		await mockEnvironmentList(page, [localEnvironment, remoteEnvironment]);
		await mockActivityReads(page, {
			'0': [activity('local-activity', '0', 'Local', 'local-network', 5)],
			'remote-activity-test': [
				activity('remote-activity', 'remote-activity-test', 'Remote Lab', 'remote-network', 1)
			]
		});

		const activityStream = waitForActivityStream(page);
		await page.goto('/dashboard');
		await activityStream;
		await page.waitForLoadState('load');

		const activityCenter = await openActivityCenter(page);
		await activityCenter.getByRole('button', { name: 'Completed' }).click();

		await expect(activityRow(activityCenter, 'local-network')).toBeVisible();
		await expect(activityRow(activityCenter, 'remote-network')).toBeVisible();
		await expect(activityCenter.getByText('Local').first()).toBeVisible();
		await expect(activityCenter.getByText('Remote Lab').first()).toBeVisible();
		await expect(page.getByRole('button', { name: /Local/ }).first()).toBeVisible();
	});

	test('keeps reachable activity visible when a configured environment fails', async ({ page }) => {
		await preserveLocalEnvironmentSelection(page);
		await mockEnvironmentList(page, [localEnvironment, remoteEnvironment]);
		await mockActivityReads(
			page,
			{
				'0': [activity('local-activity', '0', 'Local', 'local-network', 5)]
			},
			new Set(['remote-activity-test']),
			403
		);

		const activityStream = waitForActivityStream(page);
		const remoteActivityRead = waitForActivityRead(page, 'remote-activity-test');
		await page.goto('/dashboard');
		await activityStream;
		await remoteActivityRead;
		await page.waitForLoadState('load');
		await page.waitForTimeout(250);

		await expect(
			page.locator('li[data-sonner-toast]').filter({ hasText: 'Access denied' })
		).toHaveCount(0, {
			timeout: 500
		});

		const activityCenter = await openActivityCenter(page);
		await activityCenter.getByRole('button', { name: 'Completed' }).click();

		await expect(activityRow(activityCenter, 'local-network')).toBeVisible();
		await expect(activityCenter.getByText('Could not load activity from Remote Lab')).toBeVisible();
	});

	test('clears history for every configured environment and reports partial failures', async ({
		page
	}) => {
		await preserveLocalEnvironmentSelection(page);
		await mockEnvironmentList(page, [localEnvironment, remoteEnvironment]);
		await mockActivityReads(page, {
			'0': [activity('local-activity', '0', 'Local', 'local-network', 5)],
			'remote-activity-test': [
				activity('remote-activity', 'remote-activity-test', 'Remote Lab', 'remote-network', 1)
			]
		});

		const deletedEnvironments: string[] = [];
		await page.route(/\/api\/environments\/[^/]+\/activities\/history$/, async (route) => {
			const url = new URL(route.request().url());
			const match = url.pathname.match(/^\/api\/environments\/([^/]+)\/activities\/history$/);
			const environmentId = match ? decodeURIComponent(match[1]) : '';
			deletedEnvironments.push(environmentId);

			if (environmentId === 'remote-activity-test') {
				await route.fulfill({
					status: 503,
					contentType: 'application/json',
					body: JSON.stringify({ success: false, message: 'environment unavailable' })
				});
				return;
			}

			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, data: { deleted: 2 } })
			});
		});

		const activityStream = waitForActivityStream(page);
		await page.goto('/dashboard');
		await activityStream;
		await page.waitForLoadState('load');

		const activityCenter = await openActivityCenter(page);
		await activityCenter.getByRole('button', { name: 'Clear history' }).click();
		await page.getByRole('button', { name: 'Clear History', exact: true }).last().click();

		await expect.poll(() => deletedEnvironments.sort()).toEqual(['0', 'remote-activity-test']);
		await expect(
			page.getByText('Activity history partially cleared. Succeeded for 1. Failed for Remote Lab.')
		).toBeVisible();
	});

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
