import { test, expect, type Page, type Route } from '../fixtures/test.fixture';
import { openRowActionsMenu } from '../utils/table-actions.util';

test.describe('Container updates', () => {
	const UPDATES_ROUTE = '/updates';
	const CONTAINER_UPDATE_ROUTE = /\/api\/environments\/0\/containers\/[^/]+\/update\?async=true$/;
	const CONTAINER_LIST_ROUTE = /\/api\/environments\/0\/containers(?:\?.*)?$/;

	const CONTAINERS = [
		{
			id: 'update-row-a',
			name: 'updates-alpha',
			image: 'public.ecr.aws/docker/library/alpine:3.20',
			ignored: false
		},
		{
			id: 'update-row-b',
			name: 'updates-beta',
			image: 'public.ecr.aws/docker/library/busybox:1.37',
			ignored: false
		},
		{
			// Excluded from auto-update (`autoUpdateEnabled: false`): automatic runs
			// skip it, but a user may still update it explicitly (#4123).
			id: 'update-row-c',
			name: 'updates-gamma',
			image: 'public.ecr.aws/docker/library/nginx:1.27',
			ignored: true
		}
	];
	const IGNORED_CONTAINER = CONTAINERS.find((container) => container.ignored)!;

	function containerWithUpdate(container: (typeof CONTAINERS)[number]) {
		return {
			id: container.id,
			names: [`/${container.name}`],
			image: container.image,
			imageId: `sha256:${container.id}`,
			command: 'sleep',
			created: 1_700_000_000,
			labels: {},
			autoUpdateEnabled: !container.ignored,
			state: 'running',
			status: 'Up 2 hours',
			ports: [],
			hostConfig: { networkMode: 'bridge' },
			networkSettings: { networks: {} },
			mounts: [],
			updateInfo: {
				hasUpdate: true,
				updateType: 'digest',
				currentVersion: '1.0',
				latestVersion: '1.0',
				currentDigest: 'sha256:aaa',
				latestDigest: 'sha256:bbb',
				checkTime: new Date().toISOString(),
				responseTimeMs: 12
			}
		};
	}

	const TAG_UPDATE_PROJECT = {
		id: 'update-project-tag',
		name: 'updates-tag-project',
		imageRef: 'public.ecr.aws/docker/library/redis:7'
	};

	/** Serves one project whose tag moved to a new version while the tag's digest stayed the same. */
	async function stubProjectWithTagUpdate(page: Page) {
		const checkTime = new Date().toISOString();
		await page.route(/\/api\/environments\/0\/projects(?:\?.*)?$/, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					success: true,
					data: [
						{
							id: TAG_UPDATE_PROJECT.id,
							name: TAG_UPDATE_PROJECT.name,
							path: `/projects/${TAG_UPDATE_PROJECT.name}`,
							status: 'running',
							runningCount: '1',
							serviceCount: '1',
							createdAt: checkTime,
							updatedAt: checkTime,
							updateInfo: {
								status: 'has_update',
								hasUpdate: true,
								imageCount: 1,
								checkedImageCount: 1,
								imagesWithUpdates: 1,
								imagesNotPulled: 0,
								errorCount: 0,
								imageRefs: [TAG_UPDATE_PROJECT.imageRef],
								updatedImageRefs: [TAG_UPDATE_PROJECT.imageRef],
								updateInfoByRef: {
									[TAG_UPDATE_PROJECT.imageRef]: {
										hasUpdate: true,
										updateType: 'tag',
										currentVersion: '7.2.4',
										latestVersion: '7.4.1',
										currentDigest: 'sha256:same-digest',
										latestDigest: 'sha256:same-digest',
										checkTime,
										responseTimeMs: 12,
										error: ''
									}
								},
								lastCheckedAt: checkTime
							}
						}
					],
					pagination: { totalItems: 1, totalPages: 1, currentPage: 1, itemsPerPage: 20 }
				})
			});
		});
	}

	/** Serves the update-pending containers so the tab always has selectable rows; returns the number of list fetches so far. */
	async function stubContainersWithUpdates(page: Page) {
		const fetches = { count: 0 };
		await page.route(CONTAINER_LIST_ROUTE, async (route) => {
			fetches.count++;
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					success: true,
					data: CONTAINERS.map(containerWithUpdate),
					pagination: {
						totalItems: CONTAINERS.length,
						totalPages: 1,
						currentPage: 1,
						itemsPerPage: 100
					}
				})
			});
		});
		return fetches;
	}

	type ContainerUpdateOutcome =
		| { kind: 'updated' }
		| { kind: 'skipped'; reason: string }
		| { kind: 'failed'; error: string }
		| { kind: 'request-error'; error: string };

	/** Answers `POST .../containers/{id}/update` per container and records which ids were requested. */
	async function stubContainerUpdates(
		page: Page,
		outcomes: Record<string, ContainerUpdateOutcome>
	) {
		const requested: string[] = [];
		await page.route(CONTAINER_UPDATE_ROUTE, async (route: Route) => {
			const containerId = new URL(route.request().url()).pathname.split('/').at(-2)!;
			requested.push(containerId);
			const outcome = outcomes[containerId] ?? { kind: 'updated' };
			if (outcome.kind === 'request-error') {
				await route.fulfill({
					status: 500,
					contentType: 'application/json',
					json: { success: false, error: outcome.error }
				});
				return;
			}
			const container = CONTAINERS.find((candidate) => candidate.id === containerId)!;
			const now = new Date().toISOString();
			const data = {
				id: `update-activity-${requested.length}`,
				environmentId: '0',
				sourceEnvironmentId: '0',
				type: 'auto_update',
				status: 'queued',
				resourceType: 'container',
				resourceId: containerId,
				resourceName: container.name,
				createdAt: now,
				startedAt: now
			};
			await route.fulfill({
				status: 202,
				contentType: 'application/json',
				json: { success: true, data }
			});
		});
		return requested;
	}

	function containerRow(page: Page, container: (typeof CONTAINERS)[number]) {
		return page.getByRole('row').filter({ hasText: container.name });
	}

	async function selectAllContainerRows(page: Page) {
		for (const container of CONTAINERS) {
			await containerRow(page, container).getByRole('checkbox', { name: 'Select row' }).check();
		}
	}

	async function confirmBulkUpdate(page: Page) {
		await page.getByRole('button', { name: `Update (${CONTAINERS.length})`, exact: true }).click();
		const dialog = page.getByRole('dialog');
		await expect(dialog).toBeVisible();
		await dialog.getByRole('button', { name: 'Update', exact: true }).click();
	}

	/** Runs the row-level "Update Container" action for one container through its confirm dialog. */
	async function updateContainerFromRow(page: Page, container: (typeof CONTAINERS)[number]) {
		const menu = await openRowActionsMenu(page, containerRow(page, container));
		await menu.getByRole('menuitem', { name: 'Update Container', exact: true }).click();
		const dialog = page.getByRole('dialog');
		await expect(dialog).toBeVisible();
		await dialog.getByRole('button', { name: 'Update Container', exact: true }).click();
	}

	test.describe('Updates Page Project Rows', () => {
		test('shows the versions behind a tag update even when the digest is unchanged', async ({
			page
		}) => {
			await stubContainersWithUpdates(page);
			await stubProjectWithTagUpdate(page);

			await page.goto(`${UPDATES_ROUTE}?tab=projects`);
			await page.waitForLoadState('load');

			const row = page.getByRole('row').filter({ hasText: TAG_UPDATE_PROJECT.name });
			await expect(row.getByText('7.2.4', { exact: true })).toBeVisible();
			await expect(row.getByText('7.4.1', { exact: true })).toBeVisible();
			await expect(row.getByText('sha256:same-digest')).toHaveCount(0);
		});
	});

	test.describe('Updates Page Actions', () => {
		test('accepts updates for the selected container rows, including one with automatic updates disabled', async ({
			page
		}) => {
			await stubContainersWithUpdates(page);
			const requested = await stubContainerUpdates(page, {});

			await page.goto(UPDATES_ROUTE);
			await page.waitForLoadState('load');
			await expect(
				containerRow(page, IGNORED_CONTAINER).getByText('Automatic updates disabled', {
					exact: true
				})
			).toBeVisible();

			await selectAllContainerRows(page);
			await confirmBulkUpdate(page);

			await expect(
				page.getByText(`Started updates for ${CONTAINERS.length} container(s)`)
			).toBeVisible({
				timeout: 15_000
			});
			expect(requested.sort()).toEqual(CONTAINERS.map((c) => c.id).sort());
			for (const container of CONTAINERS) {
				await expect(
					containerRow(page, container).getByRole('checkbox', { name: 'Select row' })
				).not.toBeChecked();
			}
		});

		test('counts submission failures separately from later update outcomes', async ({ page }) => {
			await stubContainersWithUpdates(page);
			const [alpha, beta] = CONTAINERS;
			const requested = await stubContainerUpdates(page, {
				[beta.id]: { kind: 'failed', error: 'pull failed: registry unreachable' },
				[IGNORED_CONTAINER.id]: { kind: 'request-error', error: 'executor unavailable' }
			});

			await page.goto(UPDATES_ROUTE);
			await page.waitForLoadState('load');

			await selectAllContainerRows(page);
			await confirmBulkUpdate(page);

			await expect(
				page.getByText(`Started 2 of ${CONTAINERS.length} container update(s). 1 could not start.`)
			).toBeVisible({
				timeout: 15_000
			});
			await expect(page.getByText(`Updated ${CONTAINERS.length} resource(s)`)).toHaveCount(0);
			expect(requested.sort()).toEqual([alpha.id, beta.id, IGNORED_CONTAINER.id].sort());
		});

		test('row update acknowledges acceptance and reports submission errors', async ({ page }) => {
			await stubContainersWithUpdates(page);
			const outcomes: Record<string, ContainerUpdateOutcome> = {
				[IGNORED_CONTAINER.id]: { kind: 'skipped', reason: 'immutable image reference' }
			};
			const requested = await stubContainerUpdates(page, outcomes);

			await page.goto(UPDATES_ROUTE);
			await page.waitForLoadState('load');

			await updateContainerFromRow(page, IGNORED_CONTAINER);
			await expect(
				page.getByText(`Update started for container "${IGNORED_CONTAINER.name}"`)
			).toBeVisible({
				timeout: 15_000
			});
			await expect(page.getByText('immutable image reference')).toHaveCount(0);
			await expect(page.getByText('Up to Date')).toHaveCount(0);
			expect(requested).toEqual([IGNORED_CONTAINER.id]);

			outcomes[IGNORED_CONTAINER.id] = {
				kind: 'request-error',
				error: 'pull failed: registry unreachable'
			};
			await updateContainerFromRow(page, IGNORED_CONTAINER);
			await expect(
				page.getByText(
					`Could not confirm the update request for container "${IGNORED_CONTAINER.name}"`
				)
			).toBeVisible({
				timeout: 15_000
			});
			await expect(page.getByText('pull failed: registry unreachable')).toBeVisible();
			expect(requested).toEqual([IGNORED_CONTAINER.id, IGNORED_CONTAINER.id]);

			// The action is usable again after both outcomes: loading state was cleared.
			const menu = await openRowActionsMenu(page, containerRow(page, IGNORED_CONTAINER));
			await expect(
				menu.getByRole('menuitem', { name: 'Update Container', exact: true })
			).toBeEnabled();
			await page.keyboard.press('Escape');
		});

		test('Update All confirms before dispatching a host-wide updater run', async ({ page }) => {
			await stubContainersWithUpdates(page);

			let runs = 0;
			await page.route(/\/api\/environments\/0\/updater\/run$/, async (route) => {
				runs++;
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: { checked: 2, updated: 2, skipped: 0, failed: 0, items: [], duration: '2s' }
					})
				});
			});

			await page.goto(UPDATES_ROUTE);
			await page.waitForLoadState('load');

			await page.getByRole('button', { name: 'Update All', exact: true }).click();

			const dialog = page.getByRole('dialog');
			await expect(dialog).toBeVisible();
			await expect(dialog.getByText(/every pending update on this host/i)).toBeVisible();
			expect(runs).toBe(0);

			await dialog.getByRole('button', { name: 'Update All', exact: true }).click();

			await expect(page.getByText('Applied 2 update(s)')).toBeVisible({ timeout: 15_000 });
			expect(runs).toBe(1);
		});
	});
});

test.describe('Arcane upgrades', () => {
	const TOKEN_EXPIRY_KEY = 'arcane_token_expiry';
	const REFRESH_COOKIE = 'arcane_refresh_test=complete';
	const UPGRADE_CHANNEL = 'arcane-upgrade';
	const START_ROUTE = /\/api\/environments\/0\/system\/upgrade\/jobs$/;
	const CURRENT_ROUTE = /\/api\/environments\/0\/system\/upgrade\/jobs\/current$/;
	const STREAM_ROUTE = /\/api\/stream(?:\?.*)?$/;
	const reloadCounts = new WeakMap<Page, number>();

	type ManagerStatus = 'updated' | 'failed' | 'updating';
	type JobStatus = 'running' | 'completed' | 'failed';
	type UpgradeJobFixture = ReturnType<typeof upgradeJob>;
	type UpgradeServer = { job: UpgradeJobFixture | null; startCalls: number };

	function upgradeJob(status: JobStatus, managerStatus: ManagerStatus) {
		return {
			id: 'playwright-upgrade',
			status,
			userId: 'playwright',
			username: 'playwright',
			managerVersionAtStart: '1.0.0',
			managerDigestAtStart: '',
			managerTargetVersion: '2.0.0',
			results: [
				{
					environmentId: '0',
					environmentName: 'Manager',
					status: managerStatus,
					percent: managerStatus === 'updating' ? 50 : 100,
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

	function registerReloadCounter(page: Page) {
		reloadCounts.set(page, 0);
		page.on('domcontentloaded', () => {
			reloadCounts.set(page, (reloadCounts.get(page) ?? 0) + 1);
		});
	}

	async function registerTokenSeeding(page: Page) {
		await page.addInitScript((expiryKey: string) => {
			if (!localStorage.getItem(expiryKey)) {
				localStorage.setItem(expiryKey, new Date(Date.now() + 3_600_000).toISOString());
			}
		}, TOKEN_EXPIRY_KEY);
	}

	async function fulfillJob(route: Route, job: UpgradeJobFixture) {
		await route.fulfill({
			status: route.request().method() === 'POST' ? 202 : 200,
			contentType: 'application/json',
			body: JSON.stringify({ success: true, data: job })
		});
	}

	// Huma renders errors as RFC 7807 problem documents; the dialog surfaces `detail`.
	async function fulfillProblem(route: Route, status: number, detail: string) {
		await route.fulfill({
			status,
			contentType: 'application/problem+json',
			body: JSON.stringify({ title: 'Error', status, detail })
		});
	}

	async function fulfillVersionMismatch(route: Route) {
		await route.fulfill({
			status: 401,
			contentType: 'application/json',
			body: JSON.stringify({ message: 'Application has been updated. Refreshing session.' })
		});
	}

	async function fulfillStream(route: Route, events: Record<string, unknown>[]) {
		const timestamp = new Date().toISOString();
		await route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			body: events.map((event) => `data: ${JSON.stringify({ ...event, timestamp })}\n\n`).join('')
		});
	}

	function subscribesToUpgrades(route: Route) {
		const channels = new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
		return channels.includes(UPGRADE_CHANNEL);
	}

	function hasRefreshCookie(route: Route) {
		return route.request().headers()['cookie']?.includes(REFRESH_COOKIE) ?? false;
	}

	function upgradeEvent(job: UpgradeJobFixture) {
		return { channel: UPGRADE_CHANNEL, upgrade: job };
	}

	function newBuildEvent() {
		return { channel: 'version', version: { type: 'snapshot', info: versionInfo('2.0.0') } };
	}

	async function routeUpgradeJobs(
		page: Page,
		onStart: (route: Route, server: UpgradeServer) => Promise<void>
	) {
		const server: UpgradeServer = { job: null, startCalls: 0 };
		await page.route(START_ROUTE, async (route) => {
			server.startCalls++;
			await onStart(route, server);
		});
		await page.route(CURRENT_ROUTE, async (route) => {
			if (!server.job) {
				await fulfillProblem(route, 404, 'no upgrade job found');
				return;
			}
			await fulfillJob(route, server.job);
		});
		await page.route(STREAM_ROUTE, async (route) => {
			if (!subscribesToUpgrades(route)) {
				await route.continue();
				return;
			}
			await fulfillStream(route, server.job ? [upgradeEvent(server.job)] : []);
		});
		return server;
	}

	async function routeUpgradeStream(page: Page, handle: (route: Route) => Promise<boolean>) {
		await page.route(STREAM_ROUTE, async (route) => {
			if (!subscribesToUpgrades(route) || !(await handle(route))) {
				await route.fallback();
			}
		});
	}

	function startAndRun(job: UpgradeJobFixture) {
		return async (route: Route, server: UpgradeServer) => {
			server.job = job;
			await fulfillJob(route, upgradeJob('running', 'updating'));
		};
	}

	async function openAndConfirmUpdateAll(page: Page) {
		await page.getByRole('button', { name: 'Update All', exact: true }).first().click();
		const dialog = page.getByRole('dialog');
		await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();
		await dialog.getByRole('button', { name: 'Update All', exact: true }).click();
	}

	function currentReloadCount(page: Page) {
		return reloadCounts.get(page) ?? 0;
	}

	test.describe('Update All startup', () => {
		const CONFLICT_DETAIL = 'an upgrade job is already in progress';

		test('a failed start surfaces the API error and returns to the confirm step', async ({
			page
		}) => {
			const server = await routeUpgradeJobs(page, async (route) => {
				await fulfillProblem(route, 500, 'Failed to initiate upgrade: pull access denied');
			});

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			const toast = page
				.locator('li[data-sonner-toast]')
				.filter({ hasText: 'Failed to start update all' });
			await expect(toast).toBeVisible({ timeout: 10_000 });
			await expect(toast).toContainText('pull access denied');

			const dialog = page.getByRole('dialog');
			await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();
			await expect(dialog.getByRole('button', { name: 'Update All', exact: true })).toBeVisible();
			expect(server.startCalls).toBe(1);
		});

		test('a 409 conflict adopts the active job and follows it to completion', async ({ page }) => {
			const server = await routeUpgradeJobs(page, async (route, current) => {
				current.job = upgradeJob('running', 'updating');
				await fulfillProblem(route, 409, CONFLICT_DETAIL);
			});

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			const dialog = page.getByRole('dialog');
			await expect(dialog.getByRole('heading', { name: 'Updating environments…' })).toBeVisible({
				timeout: 10_000
			});
			server.job = upgradeJob('completed', 'updated');

			await expect(dialog.getByRole('heading', { name: 'All environments processed' })).toBeVisible(
				{
					timeout: 10_000
				}
			);
			await expect(dialog.getByText('1 of 1', { exact: true })).toBeVisible();
			await expect(page.locator('li[data-sonner-toast]')).toHaveCount(0);
			// The conflict is never retried; the existing job is tracked instead.
			expect(server.startCalls).toBe(1);
		});

		test('a 409 conflict without an active job reports the conflict and does not retry', async ({
			page
		}) => {
			const server = await routeUpgradeJobs(page, async (route) => {
				await fulfillProblem(route, 409, CONFLICT_DETAIL);
			});
			server.job = upgradeJob('completed', 'updated');

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			const toast = page
				.locator('li[data-sonner-toast]')
				.filter({ hasText: 'Failed to start update all' });
			await expect(toast).toBeVisible({ timeout: 10_000 });
			await expect(toast).toContainText(CONFLICT_DETAIL);

			const dialog = page.getByRole('dialog');
			await expect(dialog.getByRole('heading', { name: 'Update all environments' })).toBeVisible();
			expect(server.startCalls).toBe(1);
		});

		test('closing during a pending start does not reopen the dialog', async ({ page }) => {
			let releaseStart!: () => void;
			const startReleased = new Promise<void>((resolve) => {
				releaseStart = resolve;
			});
			const server = await routeUpgradeJobs(page, async (route, current) => {
				await startReleased;
				current.job = upgradeJob('running', 'updating');
				await fulfillJob(route, current.job);
			});

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			await expect.poll(() => server.startCalls).toBe(1);
			const dialog = page.getByRole('dialog');
			await expect(dialog.getByRole('heading', { name: 'Updating environments…' })).toBeVisible();
			await dialog.getByRole('button', { name: 'Close', exact: true }).first().click();
			await expect(dialog).toBeHidden();

			const startResponse = page.waitForResponse(START_ROUTE);
			releaseStart();
			await (await startResponse).finished();
			await expect(dialog).toBeHidden();
			await expect(page.locator('li[data-sonner-toast]')).toHaveCount(0);
		});
	});

	test.describe('Manager self-update recovery', () => {
		test('Update All keeps the manager result visible and refreshes without a document reload', async ({
			page
		}) => {
			registerReloadCounter(page);
			await routeUpgradeJobs(page, startAndRun(upgradeJob('completed', 'updated')));

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			// The finished result stays on screen instead of the page being yanked away.
			const dialog = page.getByRole('dialog');
			await expect(dialog.getByRole('heading', { name: 'All environments processed' })).toBeVisible(
				{
					timeout: 10_000
				}
			);
			await expect.poll(() => currentReloadCount(page)).toBe(1);

			// Closing refreshes through SvelteKit, which reloads data but not the document.
			await dialog.getByRole('button', { name: 'Close', exact: true }).first().click();
			await expect(dialog).toBeHidden();
			await expect.poll(() => currentReloadCount(page)).toBe(1);
			await expect(page).toHaveURL('/environments');
		});

		test('Update All leaves failed manager results visible without reloading', async ({ page }) => {
			registerReloadCounter(page);
			await routeUpgradeJobs(page, startAndRun(upgradeJob('failed', 'failed')));

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);

			await expect(page.getByRole('heading', { name: 'Update all failed' })).toBeVisible({
				timeout: 10_000
			});
			await expect(page.getByText('Manager update failed')).toBeVisible();
			await expect.poll(() => currentReloadCount(page)).toBe(1);
		});

		test('the single-manager flow reloads once the new build is announced', async ({ page }) => {
			registerReloadCounter(page);

			await page.route(/\/api\/environments\/0\/version(?:\?.*)?$/, async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ success: true, data: versionInfo('1.0.0') })
				});
			});
			await page.route(/\/api\/environments\/0\/system\/upgrade\/check$/, async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ canUpgrade: true, error: false, message: '' })
				});
			});
			const server = await routeUpgradeJobs(page, startAndRun(upgradeJob('running', 'updating')));
			await routeUpgradeStream(page, async (route) => {
				if (!server.job) return false;
				await fulfillStream(route, [upgradeEvent(server.job), newBuildEvent()]);
				return true;
			});

			const dashboardUpgradeCheck = page.waitForResponse((response) => {
				return (
					new URL(response.url()).pathname === '/api/environments/0/system/upgrade/check' &&
					response.status() === 200
				);
			});
			await page.goto('/dashboard');
			await dashboardUpgradeCheck;
			const versionBadge = page
				.locator('main')
				.getByRole('button', { name: 'v1.0.0', exact: true });
			await expect(versionBadge).toBeVisible();
			await versionBadge.hover();
			const upgradeButton = page.getByRole('button', { name: 'Update to 2.0.0', exact: true });
			await expect(upgradeButton).toBeVisible();
			await upgradeButton.click();
			const dialog = page.getByRole('dialog');
			await dialog.getByRole('button', { name: 'Update to 2.0.0', exact: true }).click();

			await expect.poll(() => currentReloadCount(page), { timeout: 10_000 }).toBe(2);
			expect(server.startCalls).toBe(1);
			await expect(page).toHaveURL('/dashboard');
		});

		test('a transient refresh failure keeps the session and recovers on the next reconnect', async ({
			page
		}) => {
			registerReloadCounter(page);
			await registerTokenSeeding(page);

			let refreshCalls = 0;
			let releaseRefresh!: () => void;
			const refreshReleased = new Promise<void>((resolve) => {
				releaseRefresh = resolve;
			});
			await page.route(/\/api\/auth\/refresh$/, async (route) => {
				refreshCalls++;
				if (refreshCalls === 1) {
					await route.abort('connectionfailed');
					return;
				}
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
							token: 'mocked-session-token',
							refreshToken: 'mocked-refresh-token',
							expiresAt: new Date(Date.now() + 3_600_000).toISOString()
						}
					})
				});
			});

			const server = await routeUpgradeJobs(page, startAndRun(upgradeJob('running', 'updating')));
			await routeUpgradeStream(page, async (route) => {
				if (!server.job) return false;
				if (hasRefreshCookie(route)) {
					await fulfillStream(route, [newBuildEvent()]);
					return true;
				}
				await fulfillVersionMismatch(route);
				return true;
			});

			try {
				await page.goto('/environments');
				await openAndConfirmUpdateAll(page);

				await expect.poll(() => refreshCalls, { timeout: 15_000 }).toBe(2);
				expect(
					await page.evaluate((key: string) => localStorage.getItem(key), TOKEN_EXPIRY_KEY)
				).not.toBeNull();
				expect(currentReloadCount(page)).toBe(1);
				await expect(page).toHaveURL('/environments');
				releaseRefresh();

				await expect.poll(() => currentReloadCount(page), { timeout: 15_000 }).toBe(2);
				await expect(page).toHaveURL('/environments');
			} finally {
				releaseRefresh();
			}
		});

		test('a rejected refresh during an upgrade redirects to login instead of reconnecting', async ({
			page
		}) => {
			await registerTokenSeeding(page);
			let refreshCalls = 0;

			const server = await routeUpgradeJobs(page, startAndRun(upgradeJob('running', 'updating')));
			await routeUpgradeStream(page, async (route) => {
				if (!server.job) return false;
				await fulfillVersionMismatch(route);
				return true;
			});

			await page.route('**/api/auth/refresh', async (route) => {
				if (!server.job) {
					await route.continue();
					return;
				}
				refreshCalls++;
				await route.fulfill({
					status: 401,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Invalid or expired refresh token' })
				});
			});
			await page.route(/\/api\/auth\/(?:me|bootstrap)(?:\?.*)?$/, async (route) => {
				if (!server.job) {
					await route.continue();
					return;
				}
				await route.fulfill({
					status: 401,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Authentication required' })
				});
			});

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);
			await expect.poll(() => server.startCalls).toBe(1);

			await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10_000 });
			expect(refreshCalls).toBe(1);
			await expect(
				page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
			).toBeVisible();
		});

		test('a missing refresh cookie during an upgrade redirects to login instead of reconnecting', async ({
			page
		}) => {
			let refreshCalls = 0;
			await page.context().clearCookies({ name: /^(__Secure-)?refresh$/ });

			const server = await routeUpgradeJobs(page, startAndRun(upgradeJob('running', 'updating')));
			await routeUpgradeStream(page, async (route) => {
				if (!server.job) return false;
				await fulfillVersionMismatch(route);
				return true;
			});

			await page.route('**/api/auth/refresh', async (route) => {
				if (server.job) refreshCalls++;
				await route.continue();
			});
			await page.route(/\/api\/auth\/(?:me|bootstrap)(?:\?.*)?$/, async (route) => {
				if (!server.job) {
					await route.continue();
					return;
				}
				await route.fulfill({
					status: 401,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Authentication required' })
				});
			});

			await page.goto('/environments');
			await openAndConfirmUpdateAll(page);
			await expect.poll(() => server.startCalls).toBe(1);

			await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10_000 });
			expect(refreshCalls).toBe(1);
			await expect(
				page.getByRole('button', { name: 'Sign in to Arcane', exact: true })
			).toBeVisible();
		});
	});
});
