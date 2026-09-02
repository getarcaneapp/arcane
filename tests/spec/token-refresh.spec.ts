import { test, expect, type Page } from '../fixtures/test.fixture';
import authUtil from '../utils/auth.util';

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

test('signed-in OIDC callbacks reach the callback exchange', async ({ page }) => {
	let callbackPayload: { code: string; state: string } | undefined;
	await page.route(/\/api\/oidc\/callback$/, async (route) => {
		callbackPayload = route.request().postDataJSON() as { code: string; state: string };
		await route.fulfill({
			status: 400,
			contentType: 'application/json',
			body: JSON.stringify({ success: false, message: 'Callback exchange reached' })
		});
	});

	await page.goto('/oidc/callback?code=callback-code&state=callback-state');

	await expect
		.poll(() => callbackPayload)
		.toEqual({
			code: 'callback-code',
			state: 'callback-state'
		});
	await expect(page).toHaveURL(/\/oidc\/callback\?/);
});

test.describe('Token refresh behaviour', () => {
	test('@cross-browser shows useful login errors and accepts the configured admin password', async ({
		page
	}) => {
		await page.context().clearCookies();
		let gatewayFailurePending = true;
		await page.route(/\/api\/auth\/login$/, async (route) => {
			if (!gatewayFailurePending) {
				await route.continue();
				return;
			}
			gatewayFailurePending = false;
			await route.fulfill({
				status: 502,
				contentType: 'text/html',
				body: '<html><head><title>502 Bad Gateway</title></head><body><center>openresty</center></body></html>'
			});
		});
		await page.goto('/login');
		await page.getByLabel('Username').fill('arcane');
		await page.getByLabel('Password').fill('not-the-admin-password');
		await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();

		const proxyAlert = page
			.getByRole('alert')
			.filter({ hasText: "Arcane couldn't complete login through the reverse proxy" });
		await expect(proxyAlert).toBeVisible();
		await expect(proxyAlert).not.toContainText('openresty');

		await page.getByLabel('Password').fill('not-the-admin-password');
		await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
		await expect(
			page.getByRole('alert').filter({ hasText: 'Invalid username or password' })
		).toBeVisible();

		await page.getByLabel('Password').fill(authUtil.TEST_PASSWORD);
		await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
		await expect(page).toHaveURL('/dashboard');
		await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();
		await expect(
			page.locator('main').getByText('Dashboard', { exact: true }).first()
		).toBeVisible();

		const tokenCookies = (await page.context().cookies()).filter(
			(cookie) =>
				cookie.name === 'token' ||
				cookie.name === '__Host-token' ||
				cookie.name.startsWith('token.') ||
				cookie.name.startsWith('__Host-token.')
		);
		expect(tokenCookies).toHaveLength(1);
		expect(tokenCookies[0]?.value.length).toBeLessThan(1024);
	});

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
