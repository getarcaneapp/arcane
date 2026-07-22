import { test, expect, type Page, type Route } from '@playwright/test';

const REFRESH_TOKEN_KEY = 'arcane_refresh_token';
const TOKEN_EXPIRY_KEY = 'arcane_token_expiry';
const REFRESH_COOKIE = 'arcane_refresh_test=complete';
const RELOAD_COUNT_KEY = 'arcane_upgrade_reload_count';

type ManagerStatus = 'updated' | 'failed' | 'updating';
type JobStatus = 'running' | 'completed' | 'failed';

function updateAllJob(status: JobStatus, managerStatus: ManagerStatus) {
	return {
		id: 'playwright-update-all',
		status,
		results: [
			{
				environmentId: '0',
				environmentName: 'Manager',
				status: managerStatus,
				...(managerStatus === 'failed' ? { error: 'Manager update failed' } : {})
			}
		],
		createdAt: new Date().toISOString(),
		...(status === 'completed' || status === 'failed'
			? { completedAt: new Date().toISOString() }
			: {})
	};
}

function versionInfo(currentVersion: string, newestVersion = '2.0.0') {
	return {
		currentVersion,
		displayVersion: currentVersion,
		revision: currentVersion === newestVersion ? 'new-revision' : 'old-revision',
		shortRevision: currentVersion === newestVersion ? 'new-rev' : 'old-rev',
		goVersion: 'go-test',
		nodeVersion: 'node-test',
		svelteKitVersion: 'svelte-test',
		isSemverVersion: true,
		newestVersion,
		updateAvailable: currentVersion !== newestVersion
	};
}

function dashboardSnapshot() {
	const pagination = { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 };
	return {
		containers: {
			data: [],
			pagination,
			counts: { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 }
		},
		images: { data: [], pagination },
		imageUsageCounts: { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 },
		actionItems: { items: [] },
		settings: {},
		versionInfo: versionInfo('1.0.0')
	};
}

async function registerReloadCounter(page: Page) {
	await page.addInitScript((key: string) => {
		const count = Number(sessionStorage.getItem(key) ?? '0');
		sessionStorage.setItem(key, String(count + 1));
	}, RELOAD_COUNT_KEY);
}

async function registerTokenSeeding(page: Page) {
	await page.addInitScript(
		({ tokenKey, expiryKey }: { tokenKey: string; expiryKey: string }) => {
			if (!sessionStorage.getItem(tokenKey)) {
				sessionStorage.setItem(tokenKey, 'playwright-test-refresh-token');
				sessionStorage.setItem(expiryKey, new Date(Date.now() + 3_600_000).toISOString());
			}
		},
		{ tokenKey: REFRESH_TOKEN_KEY, expiryKey: TOKEN_EXPIRY_KEY }
	);
}

async function fulfillJob(route: Route, status: JobStatus, managerStatus: ManagerStatus) {
	await route.fulfill({
		status: route.request().method() === 'POST' ? 202 : 200,
		contentType: 'application/json',
		body: JSON.stringify({ success: true, data: updateAllJob(status, managerStatus) })
	});
}

async function openAndConfirmUpdateAll(page: Page) {
	await page.getByRole('button', { name: 'Update All', exact: true }).first().click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();
	await dialog.getByRole('button', { name: 'Update All', exact: true }).click();
}

async function currentReloadCount(page: Page) {
	return page.evaluate(
		(key: string) => Number(sessionStorage.getItem(key) ?? '0'),
		RELOAD_COUNT_KEY
	);
}

test.describe('Manager self-update recovery', () => {
	test('Update All reloads automatically when the manager is updated', async ({ page }) => {
		await registerReloadCounter(page);
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await fulfillJob(route, 'completed', 'updated');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		await expect.poll(() => currentReloadCount(page), { timeout: 10_000 }).toBe(2);
		await expect(page).toHaveURL('/environments');
	});

	test('Update All leaves failed manager results visible without reloading', async ({ page }) => {
		await registerReloadCounter(page);
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await fulfillJob(route, 'failed', 'failed');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		await expect(page.getByRole('heading', { name: 'Update all failed' })).toBeVisible({
			timeout: 10_000
		});
		await expect(page.getByText('Manager update failed')).toBeVisible();
		await expect.poll(() => currentReloadCount(page)).toBe(1);
	});

	test('the single-manager flow reloads immediately after version verification', async ({
		page
	}) => {
		await registerReloadCounter(page);
		const snapshot = dashboardSnapshot();
		let upgradeTriggered = false;

		await page.route(/\/api\/dashboard\/stream\?/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/x-json-stream',
				body: `${JSON.stringify({
					type: 'snapshot',
					environmentId: '0',
					snapshot,
					timestamp: new Date().toISOString()
				})}\n`
			});
		});
		await page.route(/\/api\/environments\/0\/dashboard(?:\?.*)?$/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, data: snapshot })
			});
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/check$/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ canUpgrade: true, error: false, message: '' })
			});
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade$/, async (route) => {
			upgradeTriggered = true;
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, message: 'Upgrade started' })
			});
		});
		await page.route(/\/api\/health$/, async (route) => {
			await route.fulfill({ status: 200, body: '' });
		});
		await page.route(/\/api\/app-version$/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(versionInfo(upgradeTriggered ? '2.0.0' : '1.0.0'))
			});
		});

		await page.goto('/dashboard');
		const versionBadge = page.getByText('v1.0.0', { exact: true }).first();
		await expect(versionBadge).toBeVisible();
		await versionBadge.hover();
		const upgradeButton = page.getByRole('button', { name: 'Update to 2.0.0', exact: true });
		await expect(upgradeButton).toBeVisible();
		await upgradeButton.click();
		const dialog = page.getByRole('dialog');
		await dialog.getByRole('button', { name: 'Update to 2.0.0', exact: true }).click();

		await expect.poll(() => currentReloadCount(page), { timeout: 10_000 }).toBe(2);
		await expect(page).toHaveURL('/dashboard');
	});

	test('status and activity 401s share one refresh before the upgrade reload', async ({ page }) => {
		await registerReloadCounter(page);
		await registerTokenSeeding(page);

		let releaseActivity: () => void = () => {};
		const activityRelease = new Promise<void>((resolve) => {
			releaseActivity = resolve;
		});
		await page.route(/\/api\/activities\/stream\?/, async (route) => {
			await activityRelease;
			if (route.request().headers()['cookie']?.includes(REFRESH_COOKIE)) {
				await route.fulfill({
					status: 200,
					contentType: 'application/x-json-stream',
					body: `${JSON.stringify({ type: 'snapshot', activities: [] })}\n`
				});
				return;
			}
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		let refreshCalls = 0;
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalls++;
			await new Promise((resolve) => setTimeout(resolve, 3500));
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

		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);
		releaseActivity();

		await expect.poll(() => currentReloadCount(page), { timeout: 15_000 }).toBe(2);
		expect(refreshCalls).toBe(1);
		await expect(page).toHaveURL('/environments');
	});

	test('a transient refresh failure keeps the token and recovers on the next poll', async ({
		page
	}) => {
		await registerReloadCounter(page);
		await registerTokenSeeding(page);

		let refreshCalls = 0;
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalls++;
			if (refreshCalls === 1) {
				await route.abort('connectionfailed');
				return;
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

		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		await expect.poll(() => refreshCalls, { timeout: 10_000 }).toBe(1);
		await expect
			.poll(() => page.evaluate((key: string) => sessionStorage.getItem(key), REFRESH_TOKEN_KEY))
			.toBe('playwright-test-refresh-token');
		await expect.poll(() => currentReloadCount(page), { timeout: 15_000 }).toBe(2);
		expect(refreshCalls).toBe(2);
	});

	test('a rejected refresh during an upgrade redirects to login instead of polling', async ({
		page
	}) => {
		await registerTokenSeeding(page);
		let refreshCalls = 0;

		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalls++;
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Invalid or expired refresh token' })
			});
		});
		await page.route(/\/api\/auth\/me$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Authentication required' })
			});
		});

		await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10_000 });
		expect(refreshCalls).toBe(1);
		await expect(
			page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
		).toBeVisible();
	});

	test('a missing refresh token during an upgrade redirects to login instead of polling', async ({
		page
	}) => {
		let refreshCalls = 0;
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalls++;
			await route.continue();
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);
		await page.route(/\/api\/auth\/me$/, async (route) => {
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Authentication required' })
			});
		});

		await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10_000 });
		expect(refreshCalls).toBe(0);
		await expect(
			page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
		).toBeVisible();
	});
});
