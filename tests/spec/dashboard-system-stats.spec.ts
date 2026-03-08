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
  gpus: [],
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
            data: JSON.stringify(statsPayload),
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

    const PatchedWebSocket = function (this: unknown, url: string | URL, protocols?: string | string[]) {
      const urlString = String(url);
      if (statsPathPattern.test(urlString)) {
        return new MockStatsWebSocket(urlString);
      }
      return protocols === undefined ? new NativeWebSocket(url) : new NativeWebSocket(url, protocols);
    } as unknown as typeof WebSocket;

    Object.defineProperties(PatchedWebSocket, {
      CONNECTING: { value: NativeWebSocket.CONNECTING },
      OPEN: { value: NativeWebSocket.OPEN },
      CLOSING: { value: NativeWebSocket.CLOSING },
      CLOSED: { value: NativeWebSocket.CLOSED },
    });
    PatchedWebSocket.prototype = NativeWebSocket.prototype;

    browserWindow.WebSocket = PatchedWebSocket;
  }, mockedStats);
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
});
