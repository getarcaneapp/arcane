import { test, expect, type Page } from '@playwright/test';

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

function collectEnvironmentRequestPaths(page: Page): string[] {
	const requestPaths: string[] = [];

	page.on('request', (request) => {
		const pathname = new URL(request.url()).pathname;
		if (pathname.startsWith('/api/environments/')) {
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

		await page.goto('/dashboard');
		await page.waitForLoadState('networkidle');

		await expect(page.getByText('12.3%', { exact: true })).toBeVisible();
		await expect(page.getByText('50.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('25.0%', { exact: true })).toBeVisible();
		await expect(page.getByText('7 CPUs', { exact: true })).toBeVisible();
		await expect(page.getByText('512 MB / 1 GB', { exact: true })).toBeVisible();
		await expect(page.getByText('256 MB / 1 GB', { exact: true })).toBeVisible();
	});

	test('loads dashboard content from the snapshot endpoint without dashboard REST fan-out', async ({
		page
	}) => {
		await mockDashboardStatsWebSocket(page);
		const requestPaths = collectEnvironmentRequestPaths(page);

		await page.goto('/dashboard');
		await page.waitForLoadState('networkidle');

		expect(
			requestPaths.some((path) => /\/api\/environments\/[^/]+\/dashboard$/.test(path))
		).toBeTruthy();

		for (const blockedPattern of [
			/\/api\/environments\/[^/]+\/containers$/,
			/\/api\/environments\/[^/]+\/containers\/counts$/,
			/\/api\/environments\/[^/]+\/images$/,
			/\/api\/environments\/[^/]+\/images\/counts$/,
			/\/api\/environments\/[^/]+\/dashboard\/action-items$/,
			/\/api\/environments\/[^/]+\/system\/docker\/info$/
		]) {
			expect(countMatchingRequests(requestPaths, blockedPattern)).toBe(0);
		}
	});

	test('lazy loads docker info when the inspect dialog opens and reuses the cached result', async ({
		page
	}) => {
		await mockDashboardStatsWebSocket(page);
		const requestPaths = collectEnvironmentRequestPaths(page);

		await page.goto('/dashboard');
		await page.waitForLoadState('networkidle');

		expect(
			countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
		).toBe(0);

		const dockerInfoRequest = page.waitForRequest((request) =>
			/\/api\/environments\/[^/]+\/system\/docker\/info$/.test(new URL(request.url()).pathname)
		);
		await page
			.getByRole('button', { name: /^Inspect$/ })
			.first()
			.click();
		await dockerInfoRequest;
		await expect(page.getByRole('dialog')).toBeVisible();
		await expect
			.poll(() =>
				countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
			)
			.toBe(1);

		await page.getByRole('button', { name: /^Close$/ }).click();
		await expect(page.getByRole('dialog')).not.toBeVisible();

		await page
			.getByRole('button', { name: /^Inspect$/ })
			.first()
			.click();
		await page.waitForTimeout(300);

		expect(
			countMatchingRequests(requestPaths, /\/api\/environments\/[^/]+\/system\/docker\/info$/)
		).toBe(1);
	});
});
