import { test, expect, type Page } from '@playwright/test';

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

async function mockDashboardStatsWebSocket(page: Page) {
	await page.addInitScript((statsPayload) => {
		const browserWindow = globalThis as typeof globalThis & {
			WebSocket: any;
			EventTarget: any;
			Event: any;
			MessageEvent: any;
			CloseEvent: any;
		};
		const NativeWebSocket = browserWindow.WebSocket;
		const statsPathPattern = /\/api\/environments\/[^/]+\/ws\/system\/stats(?:\?.*)?$/;

		class MockStatsWebSocket extends browserWindow.EventTarget {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;

			url: string;
			readyState = MockStatsWebSocket.CONNECTING;
			bufferedAmount = 0;
			extensions = '';
			protocol = '';
			binaryType = 'blob';
			onopen: ((event: unknown) => void) | null = null;
			onmessage: ((event: unknown) => void) | null = null;
			onerror: ((event: unknown) => void) | null = null;
			onclose: ((event: unknown) => void) | null = null;

			constructor(url: string | URL) {
				super();
				this.url = String(url);

				queueMicrotask(() => {
					if (this.readyState !== MockStatsWebSocket.CONNECTING) return;
					this.readyState = MockStatsWebSocket.OPEN;
					const openEvent = new browserWindow.Event('open');
					this.dispatchEvent(openEvent);
					this.onopen?.(openEvent);

					const messageEvent = new browserWindow.MessageEvent('message', {
						data: JSON.stringify(statsPayload)
					});
					this.dispatchEvent(messageEvent);
					this.onmessage?.(messageEvent);
				});
			}

			send(_data?: string | ArrayBufferLike | Blob | ArrayBufferView) {}

			close(code = 1000, reason = '') {
				if (this.readyState === MockStatsWebSocket.CLOSED) return;
				this.readyState = MockStatsWebSocket.CLOSED;
				const closeEvent = new browserWindow.CloseEvent('close', { code, reason, wasClean: true });
				this.dispatchEvent(closeEvent);
				this.onclose?.(closeEvent);
			}
		}

		const PatchedWebSocket = function (
			this: unknown,
			url: string | URL,
			protocols?: string | string[]
		) {
			const urlString = String(url);
			if (statsPathPattern.test(urlString)) {
				return new MockStatsWebSocket(urlString);
			}
			return protocols === undefined
				? new NativeWebSocket(url)
				: new NativeWebSocket(url, protocols);
		} as unknown as typeof WebSocket;

		Object.defineProperties(PatchedWebSocket, {
			CONNECTING: { value: NativeWebSocket.CONNECTING },
			OPEN: { value: NativeWebSocket.OPEN },
			CLOSING: { value: NativeWebSocket.CLOSING },
			CLOSED: { value: NativeWebSocket.CLOSED }
		});
		PatchedWebSocket.prototype = NativeWebSocket.prototype;

		browserWindow.WebSocket = PatchedWebSocket;
	}, mockedStats);
}

async function mockInputCapabilities(page: Page, hoverNone: boolean, maxTouchPoints: number) {
	await page.addInitScript(
		({ hoverNone, maxTouchPoints }) => {
			Object.defineProperty(navigator, 'maxTouchPoints', {
				configurable: true,
				get: () => maxTouchPoints
			});

			const nativeMatchMedia = window.matchMedia.bind(window);
			window.matchMedia = (query: string) => {
				if (query !== '(hover: none)') {
					return nativeMatchMedia(query);
				}

				return {
					matches: hoverNone,
					media: query,
					onchange: null,
					addEventListener: () => undefined,
					removeEventListener: () => undefined,
					addListener: () => undefined,
					removeListener: () => undefined,
					dispatchEvent: () => true
				} as MediaQueryList;
			};
		},
		{ hoverNone, maxTouchPoints }
	);
}

function collectDashboardRequestPaths(page: Page): string[] {
	const requestPaths: string[] = [];

	page.on('request', (request) => {
		const pathname = new URL(request.url()).pathname;
		if (pathname.startsWith('/api/environments/') || pathname.startsWith('/api/dashboard/')) {
			requestPaths.push(pathname);
		}
	});

	return requestPaths;
}

function countMatchingRequests(paths: string[], pattern: RegExp): number {
	return paths.filter((path) => pattern.test(path)).length;
}

test.describe('Dashboard system stats websocket', () => {
	test('renders metrics from the system stats websocket stream', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');

		await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();
		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText('50.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('25.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('7 CPUs', { exact: true })).toBeVisible();
		await expect(page.getByText('512 MB / 1 GB', { exact: true })).toBeVisible();
		await expect(page.getByText('256 MB / 1 GB', { exact: true })).toBeVisible();
		await expect(page.locator('main').getByText('Local Docker', { exact: true })).toBeVisible();
		await expect(page.getByText('HTTP', { exact: true }).first()).toBeVisible();
	});

	test('loads dashboard content without eagerly loading docker info', async ({ page }) => {
		await mockDashboardStatsWebSocket(page);
		const requestPaths = collectDashboardRequestPaths(page);

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();

		await expect.poll(() => requestPaths).toContain('/api/dashboard/stream');

		expect(
			countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
		).toBe(0);
	});

	test('lazy loads docker info when the inspect dialog opens and reuses the cached result', async ({
		page
	}) => {
		await mockDashboardStatsWebSocket(page);
		const requestPaths = collectDashboardRequestPaths(page);

		await page.goto(defaultDashboardPath);
		await page.waitForLoadState('load');
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();

		expect(
			countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
		).toBe(0);

		await page.getByRole('button', { name: 'Inspect' }).first().click();
		await expect(page.getByRole('dialog')).toBeVisible();
		await expect
			.poll(() =>
				countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
			)
			.toBe(1);

		await page.getByRole('button', { name: /^Close$/ }).click();
		await expect(page.getByRole('dialog')).not.toBeVisible();

		await page.getByRole('button', { name: 'Inspect' }).first().click();
		await page.waitForTimeout(300);

		expect(
			countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
		).toBe(1);
	});
});

test.describe('Dashboard environment action tooltips', () => {
	test('uses hover tooltips on hover-capable devices that expose touch APIs', async ({ page }) => {
		await mockInputCapabilities(page, false, 5);
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();

		const detailsButton = page.getByRole('button', { name: 'View Details', exact: true });
		await expect(detailsButton).toHaveCount(1);
		await expect(detailsButton).toHaveAttribute('data-tooltip-trigger', '');

		await detailsButton.hover();
		await expect(page.locator('[data-slot="tooltip-content"]')).toContainText('View Details');
	});

	test('keeps one keyboard focus target and the Arcane button focus ring', async ({ page }) => {
		await mockInputCapabilities(page, false, 0);
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();

		const detailsButton = page.getByRole('button', { name: 'View Details', exact: true });
		const inspectButton = page.getByRole('button', { name: 'Inspect', exact: true });
		await expect(detailsButton).toHaveCount(1);
		await expect(inspectButton).toHaveCount(1);

		await detailsButton.focus();
		await page.keyboard.press('Tab');
		await expect(inspectButton).toBeFocused();

		const focusShadow = await inspectButton.evaluate(
			(element) => getComputedStyle(element).boxShadow
		);
		expect(focusShadow).not.toBe('none');
	});

	test('uses a popover for disabled actions when the primary input cannot hover', async ({
		page
	}) => {
		await mockInputCapabilities(page, true, 5);
		await mockDashboardStatsWebSocket(page);

		await page.goto(defaultDashboardPath);
		await expect(page.getByRole('heading', { name: 'Environment Board' })).toBeVisible();

		const useEnvironmentButton = page.getByRole('button', { name: 'Use Environment', exact: true });
		const disabledTrigger = page
			.locator('div[data-popover-trigger][data-disabled-child]')
			.filter({ has: useEnvironmentButton })
			.first();

		await expect(disabledTrigger).toBeVisible();
		await disabledTrigger.click();
		await expect(page.locator('[data-slot="popover-content"]')).toContainText('Current');
	});
});
