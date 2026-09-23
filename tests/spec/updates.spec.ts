import { test, expect, type Page, type Route } from '../fixtures/test.fixture';
import { openRowActionsMenu } from '../utils/table-actions.util';

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
async function stubContainerUpdates(page: Page, outcomes: Record<string, ContainerUpdateOutcome>) {
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
	test('accepts updates for the selected container rows, including an ignored one', async ({
		page
	}) => {
		await stubContainersWithUpdates(page);
		const requested = await stubContainerUpdates(page, {});

		await page.goto(UPDATES_ROUTE);
		await page.waitForLoadState('load');
		await expect(
			containerRow(page, IGNORED_CONTAINER).getByText('Ignored', { exact: true })
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
