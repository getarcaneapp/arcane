import { expect, test, type Page } from '../fixtures/test.fixture';

type MockSample = {
	cpuPercent: number;
	memoryUsageBytes: number;
	memoryLimitBytes: number;
	sampleTime: string;
};

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
	resourceSample?: MockSample | null;
};

const MiB = 1024 * 1024;
const GiB = 1024 * MiB;

function createSortContainer(
	id: string,
	created: number,
	sample: MockSample | null
): MockContainer {
	return {
		id,
		names: [`/${id}`],
		image: 'misc:latest',
		imageId: `image-${id}`,
		command: '',
		created,
		labels: {},
		state: 'running',
		status: 'Up 5 minutes',
		ports: [],
		hostConfig: { networkMode: 'default' },
		networkSettings: { networks: {} },
		mounts: [],
		resourceSample: sample
	};
}

function sample(
	cpuPercent: number,
	memoryUsageBytes: number,
	memoryLimitBytes: number
): MockSample {
	return { cpuPercent, memoryUsageBytes, memoryLimitBytes, sampleTime: new Date().toISOString() };
}

// Default order is created desc: small, high-percent, big-bytes, broken.
// Memory-bytes desc: big-bytes (3GiB), high-percent (800MiB), small (100MiB), broken.
// Memory-percent desc would put high-percent (80%) first: the two orders differ.
function basicFixtures() {
	return [
		createSortContainer('small', 400, sample(5, 100 * MiB, 8 * GiB)),
		createSortContainer('high-percent', 300, sample(50, 800 * MiB, 1 * GiB)),
		createSortContainer('big-bytes', 200, sample(10, 3 * GiB, 4 * GiB)),
		createSortContainer('broken', 100, null)
	];
}

// Twelve fixtures in created-desc order so page size 10 leaves the two
// heaviest containers off the default first page: they enter it once sorted
// by memory desc.
function pagedFixtures() {
	const items: MockContainer[] = [];
	for (let i = 0; i < 10; i++) {
		items.push(createSortContainer(`filler-${i}`, 20 - i, sample(i, (10 + i) * MiB, 8 * GiB)));
	}
	items.push(createSortContainer('offpage-heavy2', 2, sample(2, 4 * GiB, 8 * GiB)));
	items.push(createSortContainer('offpage-heavy', 1, sample(1, 5 * GiB, 8 * GiB)));
	return items;
}

type Scenario = {
	supported: boolean;
	fixtures: () => MockContainer[];
	failList?: boolean;
	requests: string[];
};

function installContainersMock(page: Page, scenario: Scenario) {
	return page.context().route('**/containers**', async (route) => {
		if (route.request().method() !== 'GET') {
			await route.continue();
			return;
		}
		const url = new URL(route.request().url());
		if (!/^\/api\/environments\/[^/]+\/containers$/.test(url.pathname)) {
			await route.continue();
			return;
		}
		if (scenario.failList) {
			await route.fulfill({
				status: 500,
				contentType: 'application/json',
				body: JSON.stringify({ success: false, data: { error: 'daemon exploded' } })
			});
			return;
		}

		scenario.requests.push(url.search);
		const sort = url.searchParams.get('sort') ?? '';
		const order = url.searchParams.get('order') ?? 'asc';
		const start = Number(url.searchParams.get('start') ?? '0');
		const limit = Number(url.searchParams.get('limit') ?? '20');

		let items = scenario.fixtures();
		const valueOf = (c: MockContainer) => {
			if (!c.resourceSample) return null;
			return sort === 'cpuUsage' ? c.resourceSample.cpuPercent : c.resourceSample.memoryUsageBytes;
		};
		if (sort === 'cpuUsage' || sort === 'memoryUsage') {
			items = [...items].sort((a, b) => {
				const va = valueOf(a);
				const vb = valueOf(b);
				if (va === null && vb === null) return a.id.localeCompare(b.id);
				if (va === null) return 1;
				if (vb === null) return -1;
				if (va === vb) return a.id.localeCompare(b.id);
				return order === 'desc' ? vb - va : va - vb;
			});
		}

		const safeLimit = limit > 0 ? limit : items.length;
		const pageItems = items.slice(start, start + safeLimit);

		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				success: true,
				data: pageItems,
				counts: {
					runningContainers: items.length,
					stoppedContainers: 0,
					totalContainers: items.length
				},
				pagination: {
					totalPages: Math.max(1, Math.ceil(items.length / safeLimit)),
					totalItems: items.length,
					currentPage: Math.floor(start / safeLimit) + 1,
					itemsPerPage: safeLimit,
					grandTotalItems: items.length
				},
				...(scenario.supported ? { resourceSortSupported: true } : {})
			})
		});
	});
}

async function openContainers(page: Page) {
	await page.addInitScript(() => {
		localStorage.removeItem('selectedEnvironmentId');
		localStorage.removeItem('arcane-container-table');
	});
	await page.goto('/containers');
	await page.waitForLoadState('load');
	await page.setViewportSize({ width: 1440, height: 900 });
}

function rowIds(page: Page) {
	return page
		.getByRole('row')
		.filter({ has: page.locator('a[href^="/containers/"]') })
		.locator('a[href^="/containers/"]')
		.evaluateAll((els) => els.map((el) => el.getAttribute('href')?.split('/').pop() ?? ''));
}

async function setPageSize(page: Page, size: string) {
	// The rows-per-page control is a bits-ui Select trigger, which renders as a plain
	// button rather than a combobox.
	await page.locator('[data-slot="select-trigger"]').filter({ visible: true }).first().click();
	await page.getByRole('option', { name: size, exact: true }).click();
}

async function sortByHeader(page: Page, name: string, direction: 'asc' | 'desc') {
	// The sortable header button opens a menu; the direction is chosen from it.
	await page.getByRole('columnheader', { name, exact: true }).getByRole('button').click();
	await page
		.getByRole('menuitem', { name: direction === 'desc' ? 'Desc' : 'Asc', exact: true })
		.click();
}

test('memory sort orders globally by bytes and renders matching samples', async ({ page }) => {
	const scenario: Scenario = { supported: true, fixtures: basicFixtures, requests: [] };
	await installContainersMock(page, scenario);
	await openContainers(page);

	await expect(page.getByRole('row').filter({ hasText: 'small' }).first()).toBeVisible();

	await sortByHeader(page, 'Memory Usage', 'desc');

	await expect
		.poll(() => rowIds(page), { timeout: 10000 })
		.toEqual(['big-bytes', 'high-percent', 'small', 'broken']);

	const bigBytesRow = page
		.getByRole('row')
		.filter({ has: page.getByRole('link', { name: 'big-bytes' }) });
	await expect(bigBytesRow.getByText('10.0%')).toBeVisible();
	await expect(bigBytesRow.getByText('3 GB', { exact: true })).toBeVisible();

	const brokenRow = page
		.getByRole('row')
		.filter({ has: page.getByRole('link', { name: 'broken' }) });
	// Both the CPU and memory cells fall back to the unavailable label.
	await expect(brokenRow.getByText('Unavailable', { exact: true })).toHaveCount(2);
});

test('sorted refresh pulls off-page containers into the first page', async ({ page }) => {
	const scenario: Scenario = { supported: true, fixtures: pagedFixtures, requests: [] };
	await installContainersMock(page, scenario);
	await openContainers(page);

	await setPageSize(page, '10');
	await expect
		.poll(() => rowIds(page), { timeout: 10000 })
		.toEqual([
			'filler-0',
			'filler-1',
			'filler-2',
			'filler-3',
			'filler-4',
			'filler-5',
			'filler-6',
			'filler-7',
			'filler-8',
			'filler-9'
		]);

	await sortByHeader(page, 'Memory Usage', 'desc');

	await expect
		.poll(() => rowIds(page), { timeout: 10000 })
		.toEqual([
			'offpage-heavy',
			'offpage-heavy2',
			'filler-9',
			'filler-8',
			'filler-7',
			'filler-6',
			'filler-5',
			'filler-4',
			'filler-3',
			'filler-2'
		]);
});

test('resource sort polls, suspends in background tabs, and surfaces failures', async ({
	page
}) => {
	test.setTimeout(60000);
	const scenario: Scenario = { supported: true, fixtures: basicFixtures, requests: [] };
	await installContainersMock(page, scenario);
	await openContainers(page);

	await sortByHeader(page, 'CPU Usage', 'desc');
	await expect
		.poll(() => rowIds(page), { timeout: 10000 })
		.toEqual(['high-percent', 'big-bytes', 'small', 'broken']);

	const initialRequests = scenario.requests.filter((q) => q.includes('sort=cpuUsage')).length;
	await expect
		.poll(() => scenario.requests.filter((q) => q.includes('sort=cpuUsage')).length, {
			timeout: 15000
		})
		.toBeGreaterThan(initialRequests);

	await page.evaluate(() => {
		Object.defineProperty(document, 'hidden', { value: true, configurable: true });
		document.dispatchEvent(new Event('visibilitychange'));
	});
	const frozen = scenario.requests.length;
	await page.waitForTimeout(6500);
	expect(scenario.requests.length).toBe(frozen);

	await page.evaluate(() => {
		Object.defineProperty(document, 'hidden', { value: false, configurable: true });
		document.dispatchEvent(new Event('visibilitychange'));
	});
	await expect.poll(() => scenario.requests.length, { timeout: 10000 }).toBeGreaterThan(frozen);

	scenario.failList = true;
	await expect(page.getByText('Automatic refresh failed: daemon exploded')).toBeVisible({
		timeout: 15000
	});

	scenario.failList = false;
	await expect(page.getByText('Automatic refresh failed: daemon exploded')).toBeHidden({
		timeout: 15000
	});
});

test('older agents disable resource sorting with an upgrade explanation', async ({ page }) => {
	const scenario: Scenario = { supported: false, fixtures: basicFixtures, requests: [] };
	await installContainersMock(page, scenario);
	await openContainers(page);

	await expect(page.getByText('needs a newer agent for this environment')).toBeVisible();
	await expect(
		page.getByRole('columnheader', { name: 'Memory Usage', exact: true }).getByRole('button')
	).toHaveCount(0);
	await expect(
		page.getByRole('columnheader', { name: 'CPU Usage', exact: true }).getByRole('button')
	).toHaveCount(0);
});
