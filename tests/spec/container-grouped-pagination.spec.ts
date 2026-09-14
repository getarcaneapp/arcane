import { expect, test, type Page } from '../fixtures/test.fixture';

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
		localStorage.removeItem('selectedEnvironmentId');
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
	await page.waitForLoadState('load');

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

type MockGroup = { groupName: string; items: MockContainer[] };

async function mockGroupedContainers(page: Page, groups: MockGroup[]) {
	const allContainers = groups.flatMap((group) => group.items);
	await page.context().route('**/containers**', async (route) => {
		const url = new URL(route.request().url());
		if (
			route.request().method() !== 'GET' ||
			!/^\/api\/environments\/[^/]+\/containers$/.test(url.pathname)
		) {
			await route.continue();
			return;
		}
		const response = buildContainersResponse(allContainers, 0, -1);
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(
				url.searchParams.get('groupBy') === 'project' ? { ...response, groups } : response
			)
		});
	});
}

function containerRow(page: Page, name: string) {
	return page.getByRole('row').filter({ has: page.getByRole('link', { name, exact: true }) });
}

function groupRow(page: Page, groupName: string) {
	return page.locator('table tbody tr').filter({ has: page.getByText(groupName, { exact: true }) });
}

async function expectContainersSelected(page: Page, names: string[], selected: boolean) {
	for (const name of names) {
		await expect(containerRow(page, name).getByRole('checkbox')).toHaveAttribute(
			'aria-checked',
			selected ? 'true' : 'false'
		);
	}
}

async function openGroupedContainers(page: Page, collapsed: Record<string, boolean>) {
	await page.addInitScript((collapsedState) => {
		localStorage.removeItem('selectedEnvironmentId');
		localStorage.removeItem('arcane-container-table');
		localStorage.setItem('container-groups-collapsed', JSON.stringify(collapsedState));
		localStorage.removeItem('collapsible-cards-expanded');
	}, collapsed);

	const groups: MockGroup[] = [
		{
			groupName: 'immich',
			items: [
				createContainer('immich-1', 'immich-server', 'immich', 900),
				createContainer('immich-2', 'immich-machine-learning', 'immich', 899),
				createContainer('immich-3', 'immich-redis', 'immich', 898),
				createContainer('immich-4', 'immich-postgres', 'immich', 897)
			]
		},
		{ groupName: 'alpha', items: [createContainer('alpha-1', 'alpha-service', 'alpha', 800)] },
		{
			groupName: 'beta',
			items: [
				createContainer('beta-1', 'beta-web', 'beta', 700),
				createContainer('beta-2', 'beta-db', 'beta', 699)
			]
		},
		{ groupName: 'gamma', items: [createContainer('gamma-1', 'gamma-service', 'gamma', 600)] }
	];
	await mockGroupedContainers(page, groups);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/containers');
	await page.waitForLoadState('load');
	await page.getByRole('button', { name: 'View' }).click();
	await page.getByRole('menuitemcheckbox', { name: 'Group by Project' }).click();
	await page.keyboard.press('Escape');
	await expect(groupRow(page, 'immich')).toContainText('(4)');
	await expect(page.getByRole('link', { name: 'immich-server', exact: true })).toBeVisible();
}

test.describe('grouped shift-select', () => {
	test('ranges follow group order and skip collapsed groups', async ({ page }) => {
		await openGroupedContainers(page, { immich: false, alpha: false, gamma: false });
		await expect(page.getByRole('link', { name: 'beta-web', exact: true })).toHaveCount(0);

		await containerRow(page, 'immich-machine-learning').getByRole('checkbox').click();
		await containerRow(page, 'gamma-service')
			.getByRole('checkbox')
			.click({ modifiers: ['Shift'] });

		await expectContainersSelected(
			page,
			[
				'immich-machine-learning',
				'immich-redis',
				'immich-postgres',
				'alpha-service',
				'gamma-service'
			],
			true
		);
		await expectContainersSelected(page, ['immich-server'], false);
		await expect(groupRow(page, 'beta').getByRole('checkbox')).toHaveAttribute(
			'aria-checked',
			'false'
		);
		await expect(groupRow(page, 'alpha').getByRole('checkbox')).toHaveAttribute(
			'aria-checked',
			'true'
		);
		await expect(groupRow(page, 'immich').getByRole('checkbox')).toHaveAttribute(
			'aria-checked',
			'mixed'
		);
		await expect(page.getByRole('button', { name: 'Stop (5)', exact: true })).toBeVisible();

		await groupRow(page, 'beta').click();
		await expect(page.getByRole('link', { name: 'beta-web', exact: true })).toBeVisible();
		await expectContainersSelected(page, ['beta-web', 'beta-db'], false);
	});

	test('expanded groups are included and group or collapse changes reset the anchor', async ({
		page
	}) => {
		await openGroupedContainers(page, { immich: false, alpha: false, beta: false });

		await containerRow(page, 'immich-postgres').getByRole('checkbox').click();
		await containerRow(page, 'beta-db')
			.getByRole('checkbox')
			.click({ modifiers: ['Shift'] });
		await expectContainersSelected(
			page,
			['immich-postgres', 'alpha-service', 'beta-web', 'beta-db'],
			true
		);
		await expectContainersSelected(page, ['immich-server', 'immich-redis'], false);

		await groupRow(page, 'alpha').getByRole('checkbox').click();
		await expectContainersSelected(page, ['alpha-service'], false);
		await containerRow(page, 'immich-server')
			.getByRole('checkbox')
			.click({ modifiers: ['Shift'] });
		await expectContainersSelected(page, ['immich-server'], true);
		await expectContainersSelected(page, ['immich-machine-learning', 'immich-redis'], false);

		await containerRow(page, 'immich-machine-learning').getByRole('checkbox').click();
		await groupRow(page, 'alpha').click();
		await expect(page.getByRole('link', { name: 'alpha-service', exact: true })).toHaveCount(0);
		await containerRow(page, 'beta-db')
			.getByRole('checkbox')
			.click({ modifiers: ['Shift'] });
		await expectContainersSelected(page, ['immich-machine-learning', 'beta-web'], true);
		await expectContainersSelected(page, ['immich-redis', 'beta-db'], false);
		await expect(page.getByRole('button', { name: 'Stop (4)', exact: true })).toBeVisible();
	});
});
