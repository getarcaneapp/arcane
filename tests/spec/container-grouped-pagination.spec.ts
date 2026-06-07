import { expect, test } from '@playwright/test';

type MockContainer = {
	id: string;
	names: string[];
	image: string;
	imageId: string;
	command: string;
	created: number;
	labels: Record<string, string>;
	state: string;
	status: string;
	ports: [];
	hostConfig: { networkMode: string };
	networkSettings: { networks: Record<string, unknown> };
	mounts: [];
};

function createContainer(
	id: string,
	name: string,
	project: string,
	created: number
): MockContainer {
	return {
		id,
		names: [`/${name}`],
		image: `${project || 'misc'}:latest`,
		imageId: `image-${id}`,
		command: '',
		created,
		labels: project ? { 'com.docker.compose.project': project } : {},
		state: 'running',
		status: 'Up 5 minutes',
		ports: [],
		hostConfig: { networkMode: 'default' },
		networkSettings: { networks: {} },
		mounts: []
	};
}

function buildContainersResponse(containers: MockContainer[], start: number, limit: number) {
	const safeLimit = limit > 0 ? limit : containers.length;
	const pageItems = limit === -1 ? containers : containers.slice(start, start + safeLimit);
	const currentPage = limit > 0 ? Math.floor(start / safeLimit) + 1 : 1;
	const totalPages = limit > 0 ? Math.max(1, Math.ceil(containers.length / safeLimit)) : 1;
	const itemsPerPage = limit === -1 ? containers.length : safeLimit;

	return {
		success: true,
		data: pageItems,
		counts: {
			runningContainers: containers.length,
			stoppedContainers: 0,
			totalContainers: containers.length
		},
		pagination: {
			totalPages,
			totalItems: containers.length,
			currentPage,
			itemsPerPage,
			grandTotalItems: containers.length
		}
	};
}

test('grouped containers do not split the same project across pages', async ({ page, context }) => {
	await page.addInitScript(() => {
		localStorage.removeItem('arcane-container-table');
		localStorage.setItem('container-groups-collapsed', JSON.stringify({ immich: false }));
		localStorage.removeItem('collapsible-cards-expanded');
	});

	let groupedMockPayload:
		| {
				groups: Array<{ groupName: string; items: MockContainer[] }>;
		  }
		| undefined;

	const otherContainers = Array.from({ length: 18 }, (_, index) => {
		const project = `other-${index + 1}`;
		return createContainer(`other-${index + 1}`, `${project}-service`, project, 1_000 - index);
	});

	const immichContainers = [
		createContainer('immich-1', 'immich-server', 'immich', 900),
		createContainer('immich-2', 'immich-machine-learning', 'immich', 899),
		createContainer('immich-3', 'immich-redis', 'immich', 898),
		createContainer('immich-4', 'immich-postgres', 'immich', 897)
	];

	const allContainers = [...otherContainers, ...immichContainers];

	await context.route('**/containers**', async (route) => {
		if (route.request().method() !== 'GET') {
			await route.continue();
			return;
		}

		const url = new URL(route.request().url());
		if (!/^\/api\/environments\/[^/]+\/containers$/.test(url.pathname)) {
			await route.continue();
			return;
		}

		const start = Number(url.searchParams.get('start') ?? '0');
		const limit = Number(url.searchParams.get('limit') ?? '20');
		const groupBy = url.searchParams.get('groupBy');

		if (groupBy === 'project') {
			groupedMockPayload = {
				groups: [
					{
						groupName: 'immich',
						items: immichContainers
					},
					...otherContainers.slice(0, 18).map((container, index) => ({
						groupName: `other-${index + 1}`,
						items: [container]
					}))
				]
			};

			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					success: true,
					data: [...immichContainers, ...otherContainers.slice(0, 18)],
					groups: groupedMockPayload.groups,
					counts: {
						runningContainers: allContainers.length,
						stoppedContainers: 0,
						totalContainers: allContainers.length
					},
					pagination: {
						totalPages: 1,
						totalItems: allContainers.length,
						currentPage: 1,
						itemsPerPage: 20,
						grandTotalItems: allContainers.length
					}
				})
			});
			return;
		}

		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(buildContainersResponse(allContainers, start, limit))
		});
	});

	await page.goto('/containers');
	await page.waitForLoadState('networkidle');

	await page.setViewportSize({ width: 1440, height: 900 });

	await page.getByRole('button', { name: 'View' }).click();
	await page.getByRole('menuitemcheckbox', { name: 'Group by Project' }).click();
	await page.keyboard.press('Escape');

	await expect
		.poll(
			() =>
				groupedMockPayload?.groups.find((group) => group.groupName === 'immich')?.items.length ?? 0
		)
		.toBe(4);

	const immichGroupRow = page
		.locator('table tbody tr')
		.filter({ has: page.getByText('immich', { exact: true }) });

	await expect(immichGroupRow).toHaveCount(1);
	await expect(immichGroupRow).toContainText('immich');
	await expect(immichGroupRow).toContainText('(4)');

	await expect(page.getByRole('link', { name: 'immich-server', exact: true })).toBeVisible();
	await expect(
		page.getByRole('link', { name: 'immich-machine-learning', exact: true })
	).toBeVisible();
	await expect(page.getByRole('link', { name: 'immich-redis', exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'immich-postgres', exact: true })).toBeVisible();
});
