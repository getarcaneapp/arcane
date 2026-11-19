import fs from 'node:fs';
import path from 'node:path';
import {
	chromium,
	type Browser,
	type BrowserContext,
	type BrowserContextOptions,
	type ConsoleMessage,
	type Page,
	type Request,
	type Response
} from 'playwright';
import { expect } from 'playwright/test';
import { afterAll, inject, test as base } from 'vite-plus/test';
import { baseURL, reportDirectory } from '../utils/report';

declare module 'vite-plus/test' {
	interface ProvidedContext {
		browserEndpoint?: string;
		contextOptions: BrowserContextOptions;
	}
}

export type PageErrorRecord = {
	url: string;
	name: string;
	message: string;
	stack?: string;
	timestamp: number;
};

type TestFixtures = {
	browser: Browser;
	outputPath: (name: string) => string;
	context: BrowserContext;
	page: Page;
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

const endpoint = inject('browserEndpoint');
const browser = await (endpoint
	? chromium.connect(endpoint, { exposeNetwork: '<loopback>' })
	: chromium.launch());
afterAll(() => browser.close());

export const test = base.extend<TestFixtures>({
	browser,
	outputPath: async ({ task }, use) => {
		const directory = path.join(
			reportDirectory,
			'results',
			`${task.file.projectName}-${task.fullName.replace(/[^\w.-]+/g, '-').slice(0, 120)}-${task.id}`
		);
		await use((name) => {
			fs.mkdirSync(directory, { recursive: true });
			return path.join(directory, name);
		});
	},
	context: async ({ outputPath, task }, use) => {
		const context = await browser.newContext({
			baseURL,
			serviceWorkers: 'block',
			...inject('contextOptions')
		});
		await context.tracing.start({ screenshots: true, snapshots: true });
		await use(context);
		if (task.result?.state === 'fail') {
			for (const [index, page] of context.pages().entries()) {
				await page.screenshot({ path: outputPath(`screenshot-${index}.png`) });
			}
			await context.tracing.stop({ path: outputPath('trace.zip') });
		} else {
			await context.tracing.stop();
		}
		await context.close();
	},
	page: async ({ context }, use) => {
		await use(await context.newPage());
	},
	registerCleanup: async ({ page: _page }, use) => {
		const cleanups: Array<() => Promise<void>> = [];
		await use((cleanup) => cleanups.push(cleanup));
		const failures: unknown[] = [];
		for (const cleanup of cleanups.reverse()) {
			try {
				await cleanup();
			} catch (error) {
				failures.push(error);
			}
		}
		if (failures.length > 0) throw new AggregateError(failures, 'Test cleanup failed');
	},
	pageErrorGuard: [
		async ({ page, outputPath, task }, use) => {
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
			}

			const unexpected = collector.errors.filter(
				(error) => !allowed.some((matcher) => pageErrorMatches(error, matcher))
			);
			if (task.result?.state === 'fail' || unexpected.length > 0) {
				fs.writeFileSync(outputPath('browser-diagnostics.txt'), diagnostics.join('\n'));
			}

			if (unexpected.length > 0) {
				const details = formatPageErrors(unexpected);
				fs.writeFileSync(outputPath('unexpected-page-errors.txt'), details);
				throw new Error(`Unexpected pageerror event(s):\n\n${details}`);
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
} from 'playwright';
