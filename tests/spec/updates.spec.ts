import { test, expect, type Page, type Route } from '../fixtures/test.fixture';
import { openRowActionsMenu } from '../utils/table-actions.util';

const UPDATES_ROUTE = '/updates';
const CONTAINER_UPDATE_ROUTE = /\/api\/environments\/0\/containers\/[^/]+\/update$/;
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
		// Listed in `autoUpdateExcludedContainers`: automatic runs skip it, but a
		// user may still update it explicitly (#4123).
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

/** Marks the ignored fixture as excluded in the setting that drives the "Ignored" badge. */
async function stubExcludedContainersSetting(page: Page) {
	const excluded = CONTAINERS.filter((container) => container.ignored)
		.map((container) => container.name)
		.join(',');
	await page.route(/\/api\/environments\/0\/settings$/, async (route) => {
		const response = await route.fetch();
		const settings = ((await response.json()) as { key: string; value: string }[]).filter(
			(setting) => setting.key !== 'autoUpdateExcludedContainers'
		);
		settings.push({ key: 'autoUpdateExcludedContainers', value: excluded });
		await route.fulfill({ response, json: settings });
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

function containerUpdateItem(containerId: string, status: string, error?: string) {
	const container = CONTAINERS.find((candidate) => candidate.id === containerId)!;
	return {
		resourceId: containerId,
		resourceName: container.name,
		resourceType: 'container',
		status,
		error
	};
}

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
		const data =
			outcome.kind === 'updated'
				? {
						checked: 1,
						updated: 1,
						skipped: 0,
						failed: 0,
						items: [containerUpdateItem(containerId, 'updated')]
					}
				: outcome.kind === 'skipped'
					? {
							checked: 1,
							updated: 0,
							skipped: 1,
							failed: 0,
							items: [containerUpdateItem(containerId, 'skipped', outcome.reason)]
						}
					: {
							checked: 1,
							updated: 0,
							skipped: 0,
							failed: 1,
							items: [containerUpdateItem(containerId, 'failed', outcome.error)]
						};
		await route.fulfill({
			status: 200,
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
	test('applies updates to the selected container rows, including an ignored one', async ({
		page
	}) => {
		await stubExcludedContainersSetting(page);
		const listFetches = await stubContainersWithUpdates(page);
		const requested = await stubContainerUpdates(page, {});

		await page.goto(UPDATES_ROUTE);
		await page.waitForLoadState('load');
		await expect(
			containerRow(page, IGNORED_CONTAINER).getByText('Ignored', { exact: true })
		).toBeVisible();
		const fetchesBeforeUpdate = listFetches.count;

		await selectAllContainerRows(page);
		await confirmBulkUpdate(page);

		await expect(page.getByText(`Updated ${CONTAINERS.length} resource(s)`)).toBeVisible({
			timeout: 15_000
		});
		expect(requested.sort()).toEqual(CONTAINERS.map((c) => c.id).sort());
		expect(listFetches.count).toBeGreaterThan(fetchesBeforeUpdate);
		for (const container of CONTAINERS) {
			await expect(
				containerRow(page, container).getByRole('checkbox', { name: 'Select row' })
			).not.toBeChecked();
		}
	});

	test('does not count skipped or failed container updates as updated', async ({ page }) => {
		await stubExcludedContainersSetting(page);
		await stubContainersWithUpdates(page);
		const [alpha, beta] = CONTAINERS;
		const requested = await stubContainerUpdates(page, {
			[beta.id]: { kind: 'skipped', reason: 'immutable image reference' },
			[IGNORED_CONTAINER.id]: { kind: 'failed', error: 'pull failed: registry unreachable' }
		});

		await page.goto(UPDATES_ROUTE);
		await page.waitForLoadState('load');

		await selectAllContainerRows(page);
		await confirmBulkUpdate(page);

		await expect(
			page.getByText(`Updated 1 of ${CONTAINERS.length} resource(s), 2 failed`)
		).toBeVisible({
			timeout: 15_000
		});
		expect(requested.sort()).toEqual([alpha.id, beta.id, IGNORED_CONTAINER.id].sort());
	});

	test('row update reports a skipped reason or the server error and recovers', async ({ page }) => {
		await stubExcludedContainersSetting(page);
		const listFetches = await stubContainersWithUpdates(page);
		const outcomes: Record<string, ContainerUpdateOutcome> = {
			[IGNORED_CONTAINER.id]: { kind: 'skipped', reason: 'immutable image reference' }
		};
		const requested = await stubContainerUpdates(page, outcomes);

		await page.goto(UPDATES_ROUTE);
		await page.waitForLoadState('load');
		const fetchesBeforeUpdate = listFetches.count;

		await updateContainerFromRow(page, IGNORED_CONTAINER);
		await expect(
			page.getByText(`Container "${IGNORED_CONTAINER.name}" was not updated`)
		).toBeVisible({
			timeout: 15_000
		});
		await expect(page.getByText('immutable image reference')).toBeVisible();
		await expect(page.getByText('Up to Date')).toHaveCount(0);
		expect(requested).toEqual([IGNORED_CONTAINER.id]);
		await expect.poll(() => listFetches.count).toBeGreaterThan(fetchesBeforeUpdate);

		outcomes[IGNORED_CONTAINER.id] = {
			kind: 'request-error',
			error: 'pull failed: registry unreachable'
		};
		await updateContainerFromRow(page, IGNORED_CONTAINER);
		await expect(
			page.getByText(`Failed to update container "${IGNORED_CONTAINER.name}"`)
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
