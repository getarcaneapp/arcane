import { expect, test, type Page } from '@playwright/test';

const duplicateLogEntry = {
	time: '2026-07-25T02:00:00.000Z',
	level: 'INFO',
	message: 'duplicate diagnostic entry'
};

async function mockDiagnosticsLogsWebSocket(page: Page) {
	await page.addInitScript((logEntry) => {
		const browserWindow = globalThis as typeof globalThis & {
			WebSocket: any;
			EventTarget: any;
			Event: any;
			MessageEvent: any;
			CloseEvent: any;
		};

		const NativeWebSocket = browserWindow.WebSocket;
		const diagnosticsLogsPathPattern = /\/api\/diagnostics\/logs\/stream(?:\?.*)?$/;

		class MockDiagnosticsLogsWebSocket extends browserWindow.EventTarget {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;

			url: string;
			readyState = MockDiagnosticsLogsWebSocket.CONNECTING;
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
					if (this.readyState !== MockDiagnosticsLogsWebSocket.CONNECTING) return;

					this.readyState = MockDiagnosticsLogsWebSocket.OPEN;

					const openEvent = new browserWindow.Event('open');
					this.dispatchEvent(openEvent);
					this.onopen?.(openEvent);

					for (let index = 0; index < 2; index += 1) {
						const messageEvent = new browserWindow.MessageEvent('message', {
							data: JSON.stringify(logEntry)
						});

						this.dispatchEvent(messageEvent);
						this.onmessage?.(messageEvent);
					}
				});
			}

			send(_data?: string | ArrayBufferLike | Blob | ArrayBufferView) {}

			close(code = 1000, reason = '') {
				if (this.readyState === MockDiagnosticsLogsWebSocket.CLOSED) return;

				this.readyState = MockDiagnosticsLogsWebSocket.CLOSED;

				const closeEvent = new browserWindow.CloseEvent('close', {
					code,
					reason,
					wasClean: true
				});

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

			if (diagnosticsLogsPathPattern.test(urlString)) {
				return new MockDiagnosticsLogsWebSocket(urlString);
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
	}, duplicateLogEntry);
}

test.describe('Diagnostics logs', () => {
	test('renders identical log entries without duplicate-key failure', async ({ page }) => {
		await mockDiagnosticsLogsWebSocket(page);

		const pageErrors: Error[] = [];
		let recentLogsRequests = 0;

		page.on('pageerror', (error) => pageErrors.push(error));
		page.on('request', (request) => {
			const url = new URL(request.url());

			if (url.pathname === '/api/diagnostics/logs') {
				recentLogsRequests += 1;
			}
		});

		await page.goto('/settings/diagnostics');
		await page.waitForLoadState('domcontentloaded');

		await expect(page).toHaveURL(/\/settings\/diagnostics$/);
		await expect(page.getByText('duplicate diagnostic entry', { exact: true })).toHaveCount(2);

		expect(recentLogsRequests).toBe(0);
		expect(pageErrors.map((error) => error.message).join('\n')).not.toContain('each_key_duplicate');
	});
});
