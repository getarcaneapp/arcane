import { expect, test as base } from '@playwright/test';
import type { Page, Request } from '@playwright/test';

const NETWORK_CHANGE_ERROR = 'net::ERR_NETWORK_CHANGED';
const DYNAMIC_IMPORT_ERROR = 'Failed to fetch dynamically imported module:';
const NETWORK_CHANGE_MATCH_WINDOW_MS = 1000;

export type PageErrorRecord = {
	url: string;
	name: string;
	message: string;
	stack?: string;
	timestamp: number;
};

type PageErrorFixtures = {
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

function dynamicImportWasInterruptedByNetworkChange(
	error: PageErrorRecord,
	networkChangeTimestamps: number[]
): boolean {
	return (
		error.message.includes(DYNAMIC_IMPORT_ERROR) &&
		networkChangeTimestamps.some(
			(timestamp) => Math.abs(error.timestamp - timestamp) <= NETWORK_CHANGE_MATCH_WINDOW_MS
		)
	);
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

export const test = base.extend<PageErrorFixtures>({
	pageErrorGuard: [
		async ({ page }, use, testInfo) => {
			const collector = installPageErrorCollector(page);
			const allowed: Array<string | RegExp> = [];
			const networkChangeTimestamps: number[] = [];
			const recordNetworkChange = (request: Request) => {
				if (request.failure()?.errorText === NETWORK_CHANGE_ERROR) {
					networkChangeTimestamps.push(Date.now());
				}
			};
			page.on('requestfailed', recordNetworkChange);

			try {
				await use({ allow: (matcher) => allowed.push(matcher) });
			} finally {
				collector.stop();
				page.off('requestfailed', recordNetworkChange);
				const unexpected = collector.errors.filter((error) => {
					if (allowed.some((matcher) => pageErrorMatches(error, matcher))) return false;
					return !dynamicImportWasInterruptedByNetworkChange(error, networkChangeTimestamps);
				});

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
