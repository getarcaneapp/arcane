import { expect, test as base } from '@playwright/test';
import type { ConsoleMessage, Page, Request, Response } from '@playwright/test';

export type PageErrorRecord = {
	url: string;
	name: string;
	message: string;
	stack?: string;
	timestamp: number;
};

type TestFixtures = {
	registerCleanup: (cleanup: () => Promise<void>) => void;
	pageErrorGuard: {
		allow: (matcher: string | RegExp) => void;
	};
};

function pageErrorMatches(error: PageErrorRecord, matcher: string | RegExp): boolean {
	const value = `${error.name}: ${error.message}`;
	return typeof matcher === 'string'
		? value.includes(matcher)
		: new RegExp(matcher.source, matcher.flags).test(value);
}

export function installPageErrorCollector(page: Page) {
	const errors: PageErrorRecord[] = [];
	const record = (error: Error) => {
		errors.push({
			url: page.url(),
			name: error.name,
			message: error.message,
			stack: error.stack,
			timestamp: Date.now()
		});
	};

	page.on('pageerror', record);

	return {
		errors,
		stop: () => page.off('pageerror', record)
	};
}

export function formatPageErrors(pageErrors: PageErrorRecord[]): string {
	return pageErrors
		.map((error, index) => {
			const stack = error.stack ? `\n${error.stack}` : '';
			return `${index + 1}. ${error.name}: ${error.message}\nURL: ${error.url}${stack}`;
		})
		.join('\n\n');
}

export const test = base.extend<TestFixtures>({
	connectOptions: [
		async ({ connectOptions }, use) => {
			const wsEndpoint = process.env.ARCANE_PLAYWRIGHT_WS_ENDPOINT;
			await use(
				connectOptions ?? (wsEndpoint ? { wsEndpoint, exposeNetwork: '<loopback>' } : undefined)
			);
		},
		{ scope: 'worker' }
	],
	registerCleanup: [
		async ({ page: _page }, use, testInfo) => {
			const cleanups: Array<() => Promise<void>> = [];
			await use((cleanup) => cleanups.push(cleanup));
			for (const cleanup of cleanups.reverse()) {
				try {
					await cleanup();
				} catch (error) {
					const detail = String(error);
					await testInfo.attach('cleanup-error', { body: detail, contentType: 'text/plain' });
					expect.soft(false, detail).toBe(true);
				}
			}
		},
		{ timeout: 120_000 }
	],
	pageErrorGuard: [
		async ({ page }, use, testInfo) => {
			const collector = installPageErrorCollector(page);
			const allowed: Array<string | RegExp> = [];
			const diagnostics: string[] = [];
			const pendingConsoleDetails: Promise<void>[] = [];
			const recordConsole = (message: ConsoleMessage) => {
				if (message.type() === 'error') {
					diagnostics.push(
						`${new Date().toISOString()} console: ${message.text()} ${JSON.stringify(message.location())}`
					);
					pendingConsoleDetails.push(
						(async () => {
							for (const argument of message.args()) {
								try {
									const details = await argument.evaluate((value) =>
										value instanceof Error
											? { name: value.name, message: value.message, stack: value.stack }
											: null
									);
									if (details) diagnostics.push(JSON.stringify(details));
								} catch {
									// Navigation can dispose console handles; the text above is still retained.
								}
							}
						})()
					);
				}
			};
			const recordFailedRequest = (request: Request) => {
				const url = new URL(request.url());
				diagnostics.push(
					`${new Date().toISOString()} ${request.method()} ${url.origin}${url.pathname}: ${request.failure()?.errorText}`
				);
			};
			const recordHttpError = (response: Response) => {
				if (response.status() < 400) return;
				const url = new URL(response.url());
				diagnostics.push(
					`${new Date().toISOString()} ${response.request().method()} ${url.origin}${url.pathname}: HTTP ${response.status()}`
				);
			};
			page.on('response', recordHttpError);
			page.on('requestfailed', recordFailedRequest);
			page.on('console', recordConsole);

			try {
				await use({ allow: (matcher) => allowed.push(matcher) });
			} finally {
				collector.stop();
				page.off('requestfailed', recordFailedRequest);
				page.off('console', recordConsole);
				page.off('response', recordHttpError);
				await Promise.all(pendingConsoleDetails);
				const unexpected = collector.errors.filter(
					(error) => !allowed.some((matcher) => pageErrorMatches(error, matcher))
				);
				if (testInfo.status !== testInfo.expectedStatus || unexpected.length > 0) {
					await testInfo.attach('browser-diagnostics', {
						body: diagnostics.join('\n'),
						contentType: 'text/plain'
					});
				}

				if (unexpected.length > 0) {
					const details = formatPageErrors(unexpected);

					await testInfo.attach('unexpected-page-errors', {
						body: details,
						contentType: 'text/plain'
					});

					throw new Error(`Unexpected pageerror event(s):\n\n${details}`);
				}
			}
		},
		{ auto: true }
	]
});

export { expect };
export type {
	APIResponse,
	Browser,
	BrowserContext,
	Locator,
	Page,
	Request,
	Response,
	Route
} from '@playwright/test';
