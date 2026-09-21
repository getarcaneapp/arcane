import { test, expect, type Page, type Route } from '../fixtures/test.fixture';

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
		displayVersion: `v${currentVersion}`,
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

// Huma renders errors as RFC 7807 problem documents; the dialog surfaces `detail`.
async function fulfillProblem(route: Route, status: number, detail: string) {
	await route.fulfill({
		status,
		contentType: 'application/problem+json',
		body: JSON.stringify({ title: 'Error', status, detail })
	});
}

test.describe('Update All startup', () => {
	const CONFLICT_DETAIL = 'an update-all job is already in progress';

	test('a failed start surfaces the API error and returns to the confirm step', async ({
		page
	}) => {
		let statusCalls = 0;
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillProblem(route, 500, 'Failed to initiate upgrade: pull access denied');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			statusCalls++;
			await fulfillJob(route, 'running', 'updating');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		const toast = page
			.locator('li[data-sonner-toast]')
			.filter({ hasText: 'Failed to start update all' });
		await expect(toast).toBeVisible({ timeout: 10_000 });
		await expect(toast).toContainText('pull access denied');

		// Back at the confirm step, ready for another attempt — and no polling started.
		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();
		await expect(dialog.getByRole('button', { name: 'Update All', exact: true })).toBeVisible();
		expect(statusCalls).toBe(0);
	});

	test('a 409 conflict adopts the active job and follows it to completion', async ({ page }) => {
		let startCalls = 0;
		let statusCalls = 0;
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			startCalls++;
			await fulfillProblem(route, 409, CONFLICT_DETAIL);
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			statusCalls++;
			// First read (adoption) reports the job still running; the poll completes it.
			if (statusCalls === 1) {
				await fulfillJob(route, 'running', 'updating');
				return;
			}
			await fulfillJob(route, 'completed', 'updated');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'All environments processed' })).toBeVisible({
			timeout: 10_000
		});
		await expect(dialog.getByText('1 of 1', { exact: true })).toBeVisible();
		await expect(page.locator('li[data-sonner-toast]')).toHaveCount(0);
		// The conflict is never retried; the existing job is tracked instead.
		expect(startCalls).toBe(1);
		expect(statusCalls).toBeGreaterThanOrEqual(2);
	});

	test('a 409 conflict without an active job reports the conflict and does not retry', async ({
		page
	}) => {
		await page.clock.install();
		let startCalls = 0;
		let statusCalls = 0;
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			startCalls++;
			await fulfillProblem(route, 409, CONFLICT_DETAIL);
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			statusCalls++;
			// The guard tripped but the latest job is already terminal: nothing to adopt.
			await fulfillJob(route, 'completed', 'updated');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		const toast = page
			.locator('li[data-sonner-toast]')
			.filter({ hasText: 'Failed to start update all' });
		await expect(toast).toBeVisible({ timeout: 10_000 });
		await expect(toast).toContainText(CONFLICT_DETAIL);

		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();

		// Wait past one poll interval to prove neither polling nor a retry kicked in.
		// The three status reads are the persistence-window grace, not polling.
		await page.clock.runFor(4_000);
		expect(startCalls).toBe(1);
		expect(statusCalls).toBe(3);
	});

	test('a 409 conflict adopts a job that is persisted shortly after the conflict', async ({
		page
	}) => {
		let statusCalls = 0;
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillProblem(route, 409, CONFLICT_DETAIL);
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			statusCalls++;
			// The winning client's job row does not exist yet on the first read.
			if (statusCalls === 1) {
				await fulfillProblem(route, 404, 'no update-all job found');
				return;
			}
			await fulfillJob(
				route,
				statusCalls === 2 ? 'running' : 'completed',
				statusCalls === 2 ? 'updating' : 'updated'
			);
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'All environments processed' })).toBeVisible({
			timeout: 10_000
		});
		await expect(page.locator('li[data-sonner-toast]')).toHaveCount(0);
	});

	test('closing the dialog during a pending start ignores the late response', async ({ page }) => {
		await page.clock.install();
		let releaseStart!: () => void;
		const startReleased = new Promise<void>((resolve) => {
			releaseStart = resolve;
		});
		let statusCalls = 0;
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await startReleased;
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			statusCalls++;
			await fulfillJob(route, 'running', 'updating');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'Updating environments…' })).toBeVisible();
		await dialog.getByRole('button', { name: 'Close', exact: true }).first().click();
		await expect(dialog).toBeHidden();

		// The start response arrives after closure: it must not reopen or start polling.
		releaseStart();
		await page.clock.runFor(4_000);
		await expect(dialog).toBeHidden();
		expect(statusCalls).toBe(0);
		await expect(page.locator('li[data-sonner-toast]')).toHaveCount(0);
	});
});

test.describe('Manager self-update recovery', () => {
	test('Update All keeps the manager result visible and refreshes without a document reload', async ({
		page
	}) => {
		await registerReloadCounter(page);
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all$/, async (route) => {
			await fulfillJob(route, 'running', 'updating');
		});
		await page.route(/\/api\/environments\/0\/system\/upgrade\/all\/status$/, async (route) => {
			await fulfillJob(route, 'completed', 'updated');
		});

		await page.goto('/environments');
		await openAndConfirmUpdateAll(page);

		// The finished result stays on screen instead of the page being yanked away.
		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'All environments processed' })).toBeVisible({
			timeout: 10_000
		});
		// A hard reload here would re-run every load function while the agents are still
		// reconnecting, briefly rendering the whole fleet as offline.
		await expect.poll(() => currentReloadCount(page)).toBe(1);

		// Closing refreshes through SvelteKit, which reloads data but not the document.
		await dialog.getByRole('button', { name: 'Close', exact: true }).first().click();
		await expect(dialog).toBeHidden();
		await expect.poll(() => currentReloadCount(page)).toBe(1);
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

		await page.route(/\/api\/stream(?:\?.*)?$/, async (route) => {
			const channels =
				new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
			const timestamp = new Date().toISOString();
			await route.fulfill({
				status: 200,
				contentType: 'application/x-json-stream',
				body: channels.includes('dashboard')
					? `${JSON.stringify({
							channel: 'dashboard',
							dashboard: { type: 'snapshot', environmentId: '0', snapshot, timestamp },
							timestamp
						})}\n`
					: ''
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

		const dashboardUpgradeCheck = page.waitForResponse((response) => {
			return (
				new URL(response.url()).pathname === '/api/environments/0/system/upgrade/check' &&
				response.status() === 200
			);
		});
		await page.goto('/dashboard');
		await dashboardUpgradeCheck;
		const versionBadge = page.locator('main').getByRole('button', { name: 'v1.0.0', exact: true });
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
		await page.clock.install();
		await registerReloadCounter(page);
		await registerTokenSeeding(page);

		let releaseActivity!: () => void;
		const activityRelease = new Promise<void>((resolve) => {
			releaseActivity = resolve;
		});
		await page.route(/\/api\/stream(?:\?.*)?$/, async (route) => {
			const channels =
				new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
			if (!channels.includes('activities')) {
				await route.fulfill({
					status: 200,
					contentType: 'application/x-json-stream',
					body: ''
				});
				return;
			}
			await activityRelease;
			if (route.request().headers()['cookie']?.includes(REFRESH_COOKIE)) {
				const timestamp = new Date().toISOString();
				await route.fulfill({
					status: 200,
					contentType: 'application/x-json-stream',
					body: `${JSON.stringify({
						channel: 'activities',
						activity: { type: 'snapshot', environmentId: '0', activities: [], timestamp },
						timestamp
					})}\n`
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
		let releaseRefresh!: () => void;
		const refreshReleased = new Promise<void>((resolve) => {
			releaseRefresh = resolve;
		});
		await page.route(/\/api\/auth\/refresh$/, async (route) => {
			refreshCalls++;
			await refreshReleased;
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
			if (route.request().headers()['cookie']?.includes(REFRESH_COOKIE)) {
				await fulfillJob(route, 'completed', 'updated');
				return;
			}
			await route.fulfill({
				status: 401,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
			});
		});

		try {
			await page.goto('/environments');
			const statusUnauthorized = page.waitForResponse((response) => {
				return (
					new URL(response.url()).pathname === '/api/environments/0/system/upgrade/all/status' &&
					response.status() === 401
				);
			});
			await openAndConfirmUpdateAll(page);
			releaseActivity();
			await expect.poll(() => refreshCalls).toBe(1);
			await page.clock.runFor(3500);
			const statusResponse = await statusUnauthorized;
			await statusResponse.finished();
			// Queue this after the response event so its 401 handler has joined the
			// in-flight refresh before that refresh is released.
			await page.evaluate(() => undefined);
			expect(refreshCalls).toBe(1);
			releaseRefresh();

			await expect.poll(() => currentReloadCount(page), { timeout: 15_000 }).toBe(2);
			expect(refreshCalls).toBe(1);
			await expect(page).toHaveURL('/environments');
		} finally {
			releaseActivity();
			releaseRefresh();
		}
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
