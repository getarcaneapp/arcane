import { test, expect, type Page } from '../fixtures/test.fixture';

const UPDATES_ROUTE = '/updates';

const CONTAINERS = [
	{
		id: 'update-row-a',
		name: 'updates-alpha',
		image: 'public.ecr.aws/docker/library/alpine:3.20'
	},
	{
		id: 'update-row-b',
		name: 'updates-beta',
		image: 'public.ecr.aws/docker/library/busybox:1.37'
	}
];

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

/** Serves the two update-pending containers so the tab always has selectable rows. */
async function stubContainersWithUpdates(page: Page) {
	await page.route(/\/api\/environments\/0\/containers(?:\?.*)?$/, async (route) => {
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
}

test.describe('Updates Page Actions', () => {
	test('applies updates to the selected container rows', async ({ page }) => {
		await stubContainersWithUpdates(page);

		const updated: string[] = [];
		await page.route(/\/api\/environments\/0\/containers\/[^/]+\/update$/, async (route) => {
			updated.push(new URL(route.request().url()).pathname.split('/').at(-2)!);
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, data: { updated: 1, failed: 0, items: [] } })
			});
		});

		await page.goto(UPDATES_ROUTE);
		await page.waitForLoadState('load');

		for (const container of CONTAINERS) {
			await page
				.getByRole('row')
				.filter({ hasText: container.name })
				.getByRole('checkbox', { name: 'Select row' })
				.check();
		}

		await page.getByRole('button', { name: 'Update (2)', exact: true }).click();

		const dialog = page.getByRole('dialog');
		await expect(dialog).toBeVisible();
		await dialog.getByRole('button', { name: 'Update', exact: true }).click();

		await expect(page.getByText('Updated 2 resource(s)')).toBeVisible({ timeout: 15_000 });
		expect(updated.sort()).toEqual(CONTAINERS.map((c) => c.id).sort());
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
