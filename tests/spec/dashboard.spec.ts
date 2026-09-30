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
	hostname: 'edge-client',
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

// Environment cards expose everything but "Use" through a row actions menu.
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
		await expect(page.locator('main').getByText('Local Docker', { exact: true })).toBeVisible();
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

test.describe('Dashboard environment actions', () => {
	test('opens environment details through the row menu', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const menu = await openEnvironmentActionsMenu(page);
		await menu.getByRole('menuitem', { name: 'View Details', exact: true }).click();
		await expect(page).toHaveURL(/\/environments\/0(?:\?.*)?$/);
	});

	test('keeps the use action and the menu trigger keyboard reachable', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const useButton = page
			.locator('main')
			.getByRole('button', { name: 'Current', exact: true })
			.first();
		const menuTrigger = page
			.locator('main')
			.getByRole('button', { name: 'Open menu', exact: true })
			.first();
		await expect(useButton).toBeVisible();
		await expect(menuTrigger).toBeVisible();

		await menuTrigger.focus();
		await expect(menuTrigger).toBeFocused();

		await page.keyboard.press('Enter');
		await expect(page.getByRole('menu')).toBeVisible();
		await page.keyboard.press('Escape');
		await expect(menuTrigger).toBeFocused();
	});

	test('disables the use action for the environment already in use', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();

		const currentButton = page
			.locator('main')
			.getByRole('button', { name: 'Current', exact: true })
			.first();
		await expect(currentButton).toBeVisible();
		await expect(currentButton).toBeDisabled();
	});
});
