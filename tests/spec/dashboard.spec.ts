import type { WebSocketRoute } from '@playwright/test';
import { test, expect, type Page } from '../fixtures/test.fixture';

const defaultDashboardPath = '/dashboard';

const mockedStats = {
	cpuUsage: 12.3,
	memoryUsage: 512 * 1024 * 1024,
	memoryTotal: 1024 * 1024 * 1024,
	diskUsage: 256 * 1024 * 1024,
	diskTotal: 1024 * 1024 * 1024,
	cpuCount: 7,
	architecture: 'amd64',
	platform: 'linux',
	hostname: 'agent-client',
	gpuCount: 0,
	gpus: []
};

async function mockDashboardStatsWebSocket(
	page: Page,
	{ delaySample = false, failConnections = false, closeAfterMessage = false } = {}
) {
	const pendingSockets: WebSocketRoute[] = [];
	await page.routeWebSocket(/\/api\/environments\/[^/]+\/ws\/system\/stats(?:\?.*)?$/, (socket) => {
		if (failConnections) {
			socket.close({ code: 1011, reason: 'Stats unavailable' });
			return;
		}
		if (delaySample) {
			pendingSockets.push(socket);
			return;
		}
		socket.send(JSON.stringify(mockedStats));
		if (closeAfterMessage) {
			failConnections = true;
			socket.close({ code: 1011, reason: 'Stats unavailable' });
		}
	});
	return {
		allowConnections() {
			failConnections = false;
		},
		deliverSample() {
			for (const socket of pendingSockets) socket.send(JSON.stringify(mockedStats));
			pendingSockets.length = 0;
		}
	};
}

const mockedCounts: Record<string, Record<string, number>> = {
	containers: {
		runningContainers: 4,
		stoppedContainers: 3,
		totalContainers: 7,
		updatesAvailable: 0
	},
	images: { imagesInuse: 5, imagesUnused: 4, totalImages: 9, totalImageSize: 0 },
	projects: { updatesAvailable: 0 },
	volumes: { inuse: 1, unused: 1, total: 2 }
};

async function mockDashboardCounts(page: Page) {
	const requested: string[] = [];
	await page.route(
		/\/api\/environments\/[^/]+\/(containers|images|projects|volumes)\/counts(?:\?.*)?$/,
		async (route) => {
			const [, , , scope, domain] = new URL(route.request().url()).pathname.split('/');
			const environmentIds = decodeURIComponent(scope).split(',');
			const data = mockedCounts[domain];
			requested.push(`${domain}:${environmentIds.join(',')}`);
			await route.fulfill({
				json: {
					success: true,
					data,
					environments: environmentIds.map((id) => ({ environment: { id, name: id }, data }))
				}
			});
		}
	);
	return requested;
}

async function openEnvironmentActionsMenu(page: Page) {
	const trigger = page
		.locator('main')
		.getByRole('button', { name: 'Open menu', exact: true })
		.first();
	await expect(trigger).toBeVisible();
	await trigger.click();
	const menu = page.getByRole('menu').filter({ visible: true }).last();
	await expect(menu).toBeVisible();
	return menu;
}

test.describe('Dashboard system stats websocket', () => {
	test.beforeEach(async ({ page }) => {
		await mockDashboardCounts(page);
	});

	test('renders metrics from the system stats websocket stream', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');

		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();
		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText('50.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('25.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('7 CPUs', { exact: true })).toBeVisible();
		await expect(page.getByText('512 MB / 1 GB', { exact: true })).toBeVisible();
		await expect(page.getByText('256 MB / 1 GB', { exact: true })).toBeVisible();
		const environmentResponse = await page.request.get('/api/environments/0');
		expect(environmentResponse.ok()).toBeTruthy();
		const { data: environment }: { data: { name: string } } = await environmentResponse.json();
		await expect(page.locator('main').getByText(environment.name, { exact: true })).toBeVisible();
	});

	test('keeps skeletons until the deadline when the stream opens without a sample', async ({
		page
	}) => {
		await page.clock.install();
		const stats = await mockDashboardStatsWebSocket(page, { delaySample: true });

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		await page.clock.runFor(5_000);
		await expect(page.getByText('Live stats unavailable', { exact: true })).toHaveCount(0);
		await expect(page.getByText('12.3%', { exact: true })).toHaveCount(0);

		await page.clock.runFor(5_000);
		await expect(page.getByText('Live stats unavailable', { exact: true })).toBeVisible();
		await expect(page.getByText('12.3%', { exact: true })).toHaveCount(0);

		stats.deliverSample();
		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText('Live stats unavailable', { exact: true })).toHaveCount(0);
	});

	test('shows unavailable metrics on connection failure and recovers on refresh', async ({
		page
	}) => {
		const stats = await mockDashboardStatsWebSocket(page, { failConnections: true });

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		await expect(page.getByText('Live stats unavailable', { exact: true })).toBeVisible();
		await expect(page.getByText('12.3%', { exact: true })).toHaveCount(0);

		stats.allowConnections();
		await page.getByRole('button', { name: 'Refresh', exact: true }).click();

		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText('Live stats unavailable', { exact: true })).toHaveCount(0);
	});

	test('keeps the last sample and flags it stale after the stream disconnects', async ({
		page
	}) => {
		await mockDashboardStatsWebSocket(page, { closeAfterMessage: true });

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText("Live stats aren't updating", { exact: true })).toBeVisible();
	});

	test('inspects Docker engine information and can reopen the dialog', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);
		await page.goto(defaultDashboardPath);
		const info = await page.request.get('/api/environments/0/system/docker/info');
		expect(info.ok()).toBe(true);
		const data = await info.json();
		for (let attempt = 0; attempt < 2; attempt += 1) {
			const menu = await openEnvironmentActionsMenu(page);
			await menu.getByRole('menuitem', { name: 'Inspect', exact: true }).click();
			const dialog = page.getByRole('dialog');
			await expect(dialog.getByText(data.ServerVersion, { exact: true })).toBeVisible();
			await dialog.getByRole('button', { name: 'Close', exact: true }).click();
			await expect(dialog).toBeHidden();
		}
	});
});

test.describe('Dashboard counts', () => {
	test('renders each environment from the counts endpoints', async ({ page }) => {
		const requested = await mockDashboardCounts(page);
		const snapshotRequests: string[] = [];
		page.on('request', (request) => {
			if (/\/api\/environments\/[^/]+\/dashboard(?:\?.*)?$/.test(request.url()))
				snapshotRequests.push(request.url());
		});
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const main = page.locator('main');
		await expect(main.getByText('4/7', { exact: true })).toBeVisible();
		await expect(main.getByText('9', { exact: true }).first()).toBeVisible();
		await expect
			.poll(() => requested)
			.toEqual(expect.arrayContaining(['containers:0', 'images:0', 'projects:0', 'volumes:0']));
		expect(snapshotRequests).toEqual([]);
	});
});

test.describe('Dashboard environment actions', () => {
	test.beforeEach(async ({ page }) => {
		await mockDashboardCounts(page);
	});

	test('opens environment details through the row menu', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const menu = await openEnvironmentActionsMenu(page);
		await menu.getByRole('menuitem', { name: 'View Details', exact: true }).click();
		await expect(page).toHaveURL(/\/environments\/0(?:\?.*)?$/);
	});

	test('keeps the menu trigger keyboard reachable', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const menuTrigger = page
			.locator('main')
			.getByRole('button', { name: 'Open menu', exact: true })
			.first();
		await expect(menuTrigger).toBeVisible();

		await menuTrigger.focus();
		await expect(menuTrigger).toBeFocused();

		await page.keyboard.press('Enter');
		await expect(page.getByRole('menu')).toBeVisible();
		await page.keyboard.press('Escape');
		await expect(menuTrigger).toBeFocused();
	});
});
