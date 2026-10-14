import { test, expect, type Page } from '../fixtures/test.fixture';

const remoteEnvironment = {
	id: 'remote-preview-test',
	name: 'Remote Preview',
	apiUrl: 'https://remote-preview.example.invalid',
	status: 'online',
	enabled: true,
	isEdge: false
};

const remoteContainer = {
	id: 'remote-preview-container',
	names: ['/remote-preview-web'],
	image: 'nginx:latest',
	imageId: 'sha256:remote-preview',
	command: 'nginx',
	created: Math.floor(Date.now() / 1000),
	state: 'exited',
	status: 'Exited (0)',
	labels: {},
	ports: [],
	mounts: [],
	networkSettings: { networks: {} }
};

function paginated<T>(data: T[], counts?: Record<string, number>) {
	return {
		success: true,
		data,
		counts,
		pagination: {
			totalPages: 1,
			totalItems: data.length,
			currentPage: 1,
			itemsPerPage: 20,
			grandTotalItems: data.length
		}
	};
}

type Preferences = Record<string, unknown>;

// Serves the signed-in user with these preferences without saving them, so other specs keep the defaults.
async function withPreferences(page: Page, preferences: () => Preferences) {
	await page.route(/\/api\/auth\/me(?:\?.*)?$/, async (route) => {
		const response = await route.fetch();
		const body = await response.json();
		body.data.preferences = { ...body.data.preferences, ...preferences() };
		await route.fulfill({ response, json: body });
	});
}

async function enablePreview(page: Page) {
	await withPreferences(page, () => ({ allEnvironmentsPreview: true }));
}

// Lists the real local environment plus a mocked remote one, and keeps the live stream from replacing that list.
// The merged request gets the real local rows plus the remote row, or a remote failure while `remote.failure` is set.
async function mockRemoteEnvironment(page: Page) {
	const remote: { failure?: string } = {};
	const local = await page.request.get('/api/environments/0');
	const localEnvironment = (await local.json()).data;
	await page.route(/\/api\/stream(?:\?.*)?$/, (route) => route.abort());
	await page.route(/\/api\/environments(?:\?.*)?$/, (route) =>
		route.fulfill({ json: paginated([localEnvironment, remoteEnvironment]) })
	);
	await page.route(
		new RegExp(`/api/environments/0(?:,|%2C)${remoteEnvironment.id}/containers(?:\\?.*)?$`),
		async (route) => {
			const url = new URL(route.request().url());
			const query = new URLSearchParams(url.search);
			const descending = query.get('sort') === 'environment' && query.get('order') === 'desc';
			query.delete('sort');
			query.delete('order');
			const localPage = await (
				await page.request.get(`/api/environments/0/containers?${query}`)
			).json();
			const localRows = localPage.data.map((row: object) => ({
				...row,
				environmentId: localEnvironment.id,
				environmentName: localEnvironment.name
			}));
			const remoteRows = remote.failure
				? []
				: [
						{
							...remoteContainer,
							environmentId: remoteEnvironment.id,
							environmentName: remoteEnvironment.name
						}
					];
			const rows = descending ? [...remoteRows, ...localRows] : [...localRows, ...remoteRows];
			await route.fulfill({
				json: {
					...paginated(rows, localPage.counts),
					failedEnvironments: remote.failure
						? [
								{
									environment: { id: remoteEnvironment.id, name: remoteEnvironment.id },
									message: remote.failure
								}
							]
						: undefined
				}
			});
		}
	);
	await page.route(
		new RegExp(`/api/environments/${remoteEnvironment.id}/containers(?:\\?.*)?$`),
		(route) =>
			route.fulfill({
				json: paginated([remoteContainer], {
					runningContainers: 0,
					stoppedContainers: 1,
					totalContainers: 1
				})
			})
	);
	return remote;
}

test.describe('All environments preview', () => {
	test('the preview toggle is saved to the user preferences', async ({ page }) => {
		let saved: Preferences = {};
		await withPreferences(page, () => saved);
		await page.route(/\/api\/auth\/me\/profile(?:\?.*)?$/, async (route) => {
			saved = { ...saved, ...route.request().postDataJSON().preferences };
			const me = await (await page.request.get('/api/auth/me')).json();
			await route.fulfill({
				json: {
					success: true,
					data: { ...me.data, preferences: { ...me.data.preferences, ...saved } }
				}
			});
		});

		await page.goto('/account?tab=previews');
		const toggle = page.getByRole('switch', { name: 'All environments (preview)' });
		await expect(toggle).not.toBeChecked();

		await toggle.click();
		await expect(toggle).toBeChecked();
		expect(saved.allEnvironmentsPreview).toBe(true);

		await page.reload();
		await expect(page.getByRole('switch', { name: 'All environments (preview)' })).toBeChecked();
	});

	test('lists rows from every environment and links details to their environment', async ({
		page
	}) => {
		await enablePreview(page);
		await mockRemoteEnvironment(page);

		await page.goto('/containers');

		await expect(
			page.getByRole('columnheader', { name: 'Environment', exact: true })
		).toBeVisible();
		const remoteRow = page.getByRole('row').filter({ hasText: 'remote-preview-web' });
		await expect(remoteRow).toContainText(remoteEnvironment.name);
		await expect(remoteRow.getByRole('link', { name: 'remote-preview-web' })).toHaveAttribute(
			'href',
			`/containers/${remoteContainer.id}?env=${remoteEnvironment.id}`
		);
	});

	test('the environment filter queries only the picked environments', async ({ page }) => {
		await enablePreview(page);
		await mockRemoteEnvironment(page);

		await page.goto('/containers');
		await expect(page.getByRole('row').filter({ hasText: 'remote-preview-web' })).toBeVisible();

		const listRequests: string[] = [];
		page.on('request', (request) => {
			if (/\/api\/environments\/[^/]+\/containers(?:\?.*)?$/.test(request.url()))
				listRequests.push(request.url());
		});
		await page.getByTestId('facet-environment-trigger').click();
		await page.getByTestId(`facet-environment-option-${remoteEnvironment.id}`).click();

		await expect.poll(() => listRequests.length).toBeGreaterThan(0);
		expect(
			listRequests.every((url) => url.includes(`/api/environments/${remoteEnvironment.id}/`))
		).toBe(true);
		expect(listRequests.some((url) => /[?&]environment=/.test(url))).toBe(false);
		await expect(page.getByRole('row').filter({ hasText: 'remote-preview-web' })).toBeVisible();
	});

	test('a failed environment is named inline and can be retried', async ({ page }) => {
		await enablePreview(page);
		const remote = await mockRemoteEnvironment(page);
		remote.failure = 'remote unavailable';

		await page.goto('/containers');
		const alert = page.getByRole('alert').filter({ hasText: remoteEnvironment.name });
		await expect(alert).toBeVisible();
		await expect(page.getByRole('row').filter({ hasText: 'remote-preview-web' })).toHaveCount(0);

		remote.failure = undefined;
		await alert.getByRole('button', { name: 'Retry', exact: true }).click();

		await expect(page.getByRole('row').filter({ hasText: 'remote-preview-web' })).toBeVisible();
		await expect(alert).toHaveCount(0);
	});

	test('page-wide confirmations count environments without listing them', async ({ page }) => {
		await enablePreview(page);
		await mockRemoteEnvironment(page);

		await page.goto('/projects');
		await page
			.getByRole('button', { name: 'Update Projects', exact: true })
			.filter({ visible: true })
			.first()
			.click();

		const dialog = page.getByRole('dialog');
		await expect(dialog).toContainText('This runs on all 2 environments.');
		await expect(dialog).not.toContainText(remoteEnvironment.name);
		await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	});

	test('sorting by environment asks the server for the merged order', async ({ page }) => {
		await enablePreview(page);
		await mockRemoteEnvironment(page);
		const listRequests: string[] = [];
		page.on('request', (request) => {
			if (/\/api\/environments\/[^/]+\/containers(?:\?.*)?$/.test(request.url()))
				listRequests.push(request.url());
		});

		await page.goto('/containers');
		await expect(page.getByRole('row').filter({ hasText: 'remote-preview-web' })).toBeVisible();
		await page
			.getByRole('columnheader', { name: 'Environment', exact: true })
			.getByRole('button')
			.click();
		await page.getByRole('menuitem', { name: 'Desc', exact: true }).click();

		await expect(page.getByRole('row').nth(1)).toContainText(remoteEnvironment.name);
		expect(
			listRequests.some((url) => /[?&]sort=environment/.test(url) && /[?&]order=desc/.test(url))
		).toBe(true);
	});

	test('the dot label style shows the environment name on hover', async ({ page }) => {
		await withPreferences(page, () => ({
			allEnvironmentsPreview: true,
			environmentLabelStyle: 'dot'
		}));
		await mockRemoteEnvironment(page);

		await page.goto('/containers');
		const remoteRow = page.getByRole('row').filter({ hasText: 'remote-preview-web' });
		await expect(remoteRow).toBeVisible();
		await expect(remoteRow).not.toContainText(remoteEnvironment.name);

		await remoteRow.getByLabel(remoteEnvironment.name).hover();
		// The row shows only the dot, so the visible name is the tooltip.
		await expect(page.getByText(remoteEnvironment.name, { exact: true })).toBeVisible();
	});
});
