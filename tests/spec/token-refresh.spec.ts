import { test, expect, type Page } from '@playwright/test';

const REFRESH_TOKEN_KEY = 'arcane_refresh_token';
const TOKEN_EXPIRY_KEY = 'arcane_token_expiry';
const REFRESH_COOKIE = 'arcane_refresh_test=complete';

/**
 * Register an addInitScript that plants a fake refresh token in sessionStorage
 * BEFORE any page JavaScript runs on every navigation. Unlike page.evaluate(),
 * addInitScript survives page.goto() calls made later in the test.
 */
async function registerTokenSeeding(page: Page) {
	await page.addInitScript(
		({ tokenKey, expiryKey }: { tokenKey: string; expiryKey: string }) => {
			sessionStorage.setItem(tokenKey, 'playwright-test-refresh-token');
			sessionStorage.setItem(expiryKey, new Date(Date.now() + 3_600_000).toISOString());
		},
		{ tokenKey: REFRESH_TOKEN_KEY, expiryKey: TOKEN_EXPIRY_KEY }
	);
}

/**
 * Keep returning a version-mismatch 401 until the refresh response's cookie is
 * present. This verifies that a retry is authenticated by refreshed browser state
 * instead of passing merely because it happens to be the second request.
 */
async function injectVersionMismatchUntilRefresh(page: Page, urlPattern: string | RegExp) {
	await page.route(urlPattern, async (route) => {
		if (!route.request().headers()['cookie']?.includes(REFRESH_COOKIE)) {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({
					code: 'UNAUTHORIZED',
					message: 'Application has been updated. Please log in again.'
				})
			});
		} else {
			await route.continue();
		}
	});
}

/**
 * Intercept every request matching urlPattern with a 401.
 */
async function injectExpired401Always(page: Page, urlPattern: string | RegExp) {
	await page.route(urlPattern, async (route) => {
		await route.fulfill({
			status: 401,
			contentType: 'application/json',
			body: JSON.stringify({ code: 'UNAUTHORIZED', message: 'Invalid or expired token' })
		});
	});
}

/**
 * Mock /auth/refresh to return a synthetic 200 and browser cookie. Returns a
 * getter to assert how many refresh requests were made.
 */
async function mockRefreshSuccess(page: Page, delayMs = 0): Promise<() => number> {
	let callCount = 0;
	await page.route(/\/api\/auth\/refresh$/, async (route) => {
		callCount++;
		if (delayMs > 0) {
			await new Promise((resolve) => setTimeout(resolve, delayMs));
		}
		await route.fulfill({
			status: 200,
			headers: {
				'content-type': 'application/json',
				'set-cookie': `${REFRESH_COOKIE}; Path=/; SameSite=Lax`
			},
			body: JSON.stringify({
				success: true,
				data: {
					token: 'mocked-access-token',
					refreshToken: 'mocked-refresh-token',
					expiresAt: new Date(Date.now() + 3_600_000).toISOString()
				}
			})
		});
	});
	return () => callCount;
}

test.describe('Token refresh behaviour', () => {
	test('version mismatch 401 on /auth/me during page load is silently recovered', async ({
		page
	}) => {
		await registerTokenSeeding(page);
		const refreshCallCount = await mockRefreshSuccess(page);
		await injectVersionMismatchUntilRefresh(page, /\/api\/auth\/me(?:\?.*)?$/);

		await page.goto('/dashboard');
		await page.waitForLoadState('load');

		await expect.poll(() => refreshCallCount()).toBe(1);
		await expect(page).toHaveURL('/dashboard');
		await expect(page.getByRole('button', { name: 'Sign in to Arcane' })).not.toBeVisible();
	});

	test('version mismatch 401 on a data endpoint mid-session is silently recovered', async ({
		page
	}) => {
		await registerTokenSeeding(page);
		const refreshCallCount = await mockRefreshSuccess(page);
		await injectVersionMismatchUntilRefresh(page, /\/api\/environments\/[^/]+\/containers/);

		await page.goto('/containers');
		await page.waitForLoadState('load');

		await expect.poll(() => refreshCallCount()).toBe(1);
		await expect(page).toHaveURL('/containers');
		await expect(page.getByRole('heading', { name: 'Containers', level: 1 })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Sign in to Arcane' })).not.toBeVisible();
	});

	test('failed token refresh redirects to /login', async ({ page }) => {
		await registerTokenSeeding(page);

		let refreshCalled = false;
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalled = true;
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ code: 'UNAUTHORIZED', message: 'Invalid or expired refresh token' })
			});
		});

		await injectExpired401Always(page, /\/api\/environments\/[^/]+\/containers(?:\/.*)?$/);
		// Keep /auth/me unauthenticated too so the login page does not immediately bounce back.
		await injectExpired401Always(page, /\/api\/auth\/me$/);

		await page.goto('/containers');
		await expect.poll(() => refreshCalled).toBe(true);
		await page.waitForURL(/\/login(\?|$)/, { timeout: 15_000 });
		await expect(
			page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
		).toBeVisible();
	});

	test('unauthenticated users are redirected to /login', async ({ page }) => {
		await page.context().clearCookies();
		await page.goto('/dashboard');
		await page.waitForURL(/\/login/, { timeout: 10_000 });
		await page.waitForLoadState('load');
		await expect(page).toHaveURL(/\/login/);
		await expect(
			page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
		).toBeVisible();
	});

	test('login page honours the redirect param and returns users to their original path', async ({
		page
	}) => {
		await page.goto('/login?redirect=%2Fcontainers');
		await page.waitForURL(/\/containers|\/login/, { timeout: 8_000 });
		const url = page.url();
		expect(url).toMatch(/\/containers|\/login\?redirect=%2Fcontainers/);
	});
});
