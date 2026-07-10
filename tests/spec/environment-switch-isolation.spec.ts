import { expect, test, type Page, type Route } from '@playwright/test';

const localEnvironment = {
	id: '0',
	name: 'Local Test',
	apiUrl: 'unix:///var/run/docker.sock',
	status: 'online',
	enabled: true,
	isEdge: false
};

const remoteEnvironment = {
	id: 'remote-switch-test',
	name: 'Remote Test',
	apiUrl: 'https://remote.example.invalid',
	status: 'online',
	enabled: true,
	isEdge: false
};

function paginated<T>(data: T[]) {
	return {
		success: true,
		data,
		counts: {
			runningContainers: data.length,
			stoppedContainers: 0,
			totalContainers: data.length
		},
		pagination: {
			totalPages: 1,
			totalItems: data.length,
			currentPage: 1,
			itemsPerPage: 20,
			grandTotalItems: data.length
		}
	};
}

function containerSummary(id: string, name: string) {
	return {
		id,
		names: [`/${name}`],
		image: 'nginx:latest',
		imageId: `sha256:${id}`,
		command: 'nginx',
		created: 1_700_000_000,
		labels: {},
		state: 'running',
		status: 'Up 5 minutes',
		ports: [],
		hostConfig: { networkMode: 'default' },
		networkSettings: { networks: {} },
		mounts: []
	};
}

function containerDetails(id: string, name: string) {
	return {
		id,
		name,
		image: 'nginx:latest',
		imageId: `sha256:${id}`,
		created: '2026-01-01T00:00:00Z',
		state: {
			status: 'running',
			running: true,
			startedAt: '2026-01-01T00:00:00Z',
			finishedAt: ''
		},
		config: {},
		hostConfig: { networkMode: 'default' },
		networkSettings: { networks: {} },
		ports: [],
		mounts: [],
		labels: {},
		redeployDisabled: false
	};
}

async function mockEnvironmentCatalog(page: Page) {
	await page.addInitScript(() => {
		localStorage.removeItem('selectedEnvironmentId');
		localStorage.removeItem('arcane-container-table');
	});
	await page.context().route(/\/api\/environments(?:\?.*)?$/, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(paginated([localEnvironment, remoteEnvironment]))
		});
	});
	await page
		.context()
		.route(/\/api\/environments\/(?:0|remote-switch-test)\/settings$/, async (route) => {
			if (route.request().method() !== 'GET') {
				await route.continue();
				return;
			}
			await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
		});
}

async function selectRemoteEnvironment(page: Page) {
	await page.getByRole('button').filter({ hasText: localEnvironment.name }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Select Environment' });
	await expect(dialog).toBeVisible();
	await dialog.getByRole('button').filter({ hasText: remoteEnvironment.name }).first().click();
}

test.describe('Environment switch isolation', () => {
	test('clears local rows and stats targets while the remote response is delayed', async ({
		page
	}) => {
		let releaseRemote!: () => void;
		const remoteGate = new Promise<void>((resolve) => {
			releaseRemote = resolve;
		});
		let markRemoteStarted!: () => void;
		const remoteStarted = new Promise<void>((resolve) => {
			markRemoteStarted = resolve;
		});
		let remoteMarked = false;
		const websocketPaths: string[] = [];

		await mockEnvironmentCatalog(page);
		await page
			.context()
			.route(/\/api\/environments\/[^/]+\/containers(?:\?.*)?$/, async (route: Route) => {
				const pathname = new URL(route.request().url()).pathname;
				if (pathname === '/api/environments/remote-switch-test/containers') {
					if (!remoteMarked) {
						remoteMarked = true;
						markRemoteStarted();
					}
					await remoteGate;
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(paginated([containerSummary('remote-b-id', 'remote-b-container')]))
					});
					return;
				}
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(paginated([containerSummary('local-a-id', 'local-a-container')]))
				});
			});
		page.on('websocket', (socket) => websocketPaths.push(new URL(socket.url()).pathname));

		await page.goto('/containers');
		await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toBeVisible();

		try {
			await selectRemoteEnvironment(page);
			await remoteStarted;
			await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toHaveCount(
				0
			);
			await page.waitForTimeout(200);
			expect(websocketPaths).not.toContain(
				'/api/environments/remote-switch-test/ws/containers/local-a-id/stats'
			);
		} finally {
			releaseRemote();
		}

		await expect(page.getByRole('link', { name: 'remote-b-container', exact: true })).toBeVisible();
		await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toHaveCount(0);
	});

	test('does not restore local rows when the remote request fails', async ({ page }) => {
		await mockEnvironmentCatalog(page);
		await page
			.context()
			.route(/\/api\/environments\/[^/]+\/containers(?:\?.*)?$/, async (route: Route) => {
				const pathname = new URL(route.request().url()).pathname;
				if (pathname === '/api/environments/remote-switch-test/containers') {
					await route.fulfill({
						status: 503,
						contentType: 'application/json',
						body: JSON.stringify({ success: false, message: 'remote unavailable' })
					});
					return;
				}
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(paginated([containerSummary('local-a-id', 'local-a-container')]))
				});
			});

		await page.goto('/containers');
		await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toBeVisible();
		const failedRemoteRequest = page.waitForResponse((response) => {
			return (
				new URL(response.url()).pathname === '/api/environments/remote-switch-test/containers' &&
				response.status() === 503
			);
		});
		await selectRemoteEnvironment(page);
		await failedRemoteRequest;
		await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toHaveCount(0);
	});

	test('remounts same-route details and navigates to the unwrapped redeployed container id', async ({
		page
	}) => {
		await page.addInitScript(() => localStorage.removeItem('selectedEnvironmentId'));
		const detailsById = {
			'route-a': containerDetails('route-a', 'Route A Container'),
			'route-b': containerDetails('route-b', 'Route B Container'),
			'route-redeployed': containerDetails('route-redeployed', 'Redeployed Container')
		};

		await page.context().route(/\/api\/environments\/0\/settings$/, async (route) => {
			await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
		});
		await page.context().route(/\/api\/environments\/0\/containers\/([^/?]+)$/, async (route) => {
			const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
			const details = detailsById[id as keyof typeof detailsById];
			await route.fulfill({
				status: details ? 200 : 404,
				contentType: 'application/json',
				body: JSON.stringify(
					details ? { success: true, data: details } : { success: false, message: 'not found' }
				)
			});
		});
		await page
			.context()
			.route(/\/api\/environments\/0\/containers\/route-b\/redeploy$/, async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ success: true, data: detailsById['route-redeployed'] })
				});
			});

		await page.goto('/containers/route-a');
		await expect(page.getByRole('heading', { name: 'Route A Container' })).toBeVisible();
		await page.getByRole('tab', { name: 'Logs' }).click();
		await expect(page.getByRole('tab', { name: 'Logs' })).toHaveAttribute('aria-selected', 'true');

		await page.evaluate(() => {
			const link = document.createElement('a');
			link.id = 'same-route-navigation';
			link.href = '/containers/route-b';
			link.textContent = 'navigate';
			document.body.append(link);
		});
		await page.locator('#same-route-navigation').click();
		await expect(page).toHaveURL('/containers/route-b');
		await expect(page.getByRole('heading', { name: 'Route B Container' })).toBeVisible();
		await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute(
			'aria-selected',
			'true'
		);

		await page.getByRole('button', { name: 'Redeploy', exact: true }).click();
		const dialog = page.getByRole('dialog');
		await dialog.getByRole('button', { name: 'Redeploy', exact: true }).click();
		await expect(page).toHaveURL('/containers/route-redeployed');
		await expect(page.getByRole('heading', { name: 'Redeployed Container' })).toBeVisible();
	});
});
