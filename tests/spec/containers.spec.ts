import { stat } from 'node:fs/promises';
import { test, expect, type Page, type Locator, type Response } from '../fixtures/test.fixture';
import { readApiData, removeApiResource, type Paginated } from '../utils/fetch.util';
import { ContainerSummary } from '../types/containers.type';
import { openRowActionsMenu } from '../utils/table-actions.util';
import { waitForDialogReady } from '../utils/playwright.util';

test.describe('Management', () => {
	const CONTAINERS_ROUTE = '/containers';

	const structuredContainerLog = {
		level: 'stdout',
		message: JSON.stringify({
			level: 'info',
			message: 'structured container log marker',
			request_id: 'request-3415'
		}),
		timestamp: '2026-07-27T23:08:37.000Z'
	};

	async function mockContainerLogsWebSocket(page: Page) {
		await page.addInitScript(() => {
			localStorage.setItem('arcane_log_json_parsing_v3', 'false');
			localStorage.setItem('arcane_log_auto_start', 'false');
		});
		await page.routeWebSocket(
			/\/api\/environments\/[^/]+\/ws\/containers\/[^/]+\/logs(?:\?.*)?$/,
			(socket) => socket.send(JSON.stringify(structuredContainerLog))
		);
	}

	async function navigateToContainers(page: Page) {
		await page.goto(CONTAINERS_ROUTE);
		await page.waitForLoadState('load');
	}

	let containersData: Paginated<ContainerSummary> = { data: [], pagination: { totalItems: 0 } };

	const fixtureContainerIds = new Set<string>();

	test.describe('Containers Page', () => {
		test.beforeEach(async ({ page }) => {
			containersData = { data: [] };
			for (const state of ['running', 'exited']) {
				const name = `e2e-list-${state}-${Date.now()}`;
				const container = await readApiData<{ id: string }>(
					await page.request.post('/api/environments/0/containers', {
						data: {
							name,
							image: 'public.ecr.aws/docker/library/busybox:1.37',
							cmd: state === 'running' ? ['sleep', '3600'] : ['true']
						}
					}),
					`Create ${state} container fixture`
				);
				fixtureContainerIds.add(container.id);
				await expect
					.poll(
						async () =>
							(
								await readApiData<{ state: { status: string } }>(
									await page.request.get(`/api/environments/0/containers/${container.id}`),
									'Read fixture state'
								)
							).state.status
					)
					.toBe(state);
				containersData.data.push({ id: container.id, names: [name], state });
			}
			await navigateToContainers(page);
		});

		test.afterEach(async ({ page }) => {
			for (const id of fixtureContainerIds) {
				await removeApiResource(
					page,
					`/api/environments/0/containers/${id}?force=true&volumes=false`
				);
			}
			fixtureContainerIds.clear();
		});

		test('should display stat cards with correct counts', async ({ page }) => {
			// Aggregate counts can exceed the current page and must not depend on other workers' containers.
			const counts = { totalContainers: 37, runningContainers: 23, stoppedContainers: 14 };
			await page.route(
				(url) => /^\/api\/environments\/[^/]+\/containers$/.test(url.pathname),
				async (route) => {
					const response = await route.fetch();
					const body = (await response.json()) as Record<string, unknown>;
					await route.fulfill({ response, json: { ...body, counts } });
				}
			);

			await navigateToContainers(page);
			await expect(
				page.getByText(`${counts.totalContainers} Total`, { exact: true })
			).toBeVisible();
			await expect(
				page.getByText(`${counts.runningContainers} Running`, { exact: true })
			).toBeVisible();
			await expect(
				page.getByText(`${counts.stoppedContainers} Stopped`, { exact: true })
			).toBeVisible();
		});

		test('should navigate to container details on Inspect', async ({ page }) => {
			expect(containersData.data.length, 'No containers available').toBeGreaterThan(0);
			await navigateToContainers(page);

			const firstRow = page
				.getByRole('row')
				.filter({ has: page.getByRole('button', { name: 'Open menu', exact: true }) })
				.first();
			const menu = await openRowActionsMenu(page, firstRow);
			await menu.getByRole('menuitem', { name: 'Inspect', exact: true }).click();

			await expect(page).toHaveURL(/\/containers\/.+/);
			await expect(page.getByRole('tab', { name: 'Overview', exact: true })).toBeVisible();
			await expect(page.getByRole('heading', { name: 'Runtime', exact: true })).toBeVisible();
		});

		test('should show live CPU and memory monitors on the logs tab for running containers', async ({
			page
		}) => {
			const running = containersData.data.find((c) => c.state === 'running');
			expect(running, 'No running container available').toBeDefined();

			await page.goto(`/containers/${running!.id}`);
			await page.waitForLoadState('load');

			await page.getByRole('tab', { name: 'Logs' }).click();

			await expect(page.getByTestId('container-log-cpu-monitor')).toBeVisible();
			await expect(page.getByTestId('container-log-memory-monitor')).toBeVisible();
			await expect(page.getByTestId('container-log-cpu-monitor')).not.toContainText('N/A');
			await expect(page.getByTestId('container-log-memory-monitor')).not.toContainText('N/A');
		});

		test('keeps the parsed log toggle synchronized when structured logs are detected', async ({
			page
		}) => {
			const running = containersData.data.find((container) => container.state === 'running');
			expect(running, 'No running container available').toBeDefined();

			await mockContainerLogsWebSocket(page);
			await page.goto(`/containers/${running!.id}`);
			await page.waitForLoadState('load');
			await page.getByRole('tab', { name: 'Logs' }).click();

			const parsedModeToggle = page.locator('#parsed-log-mode-toggle:visible');
			await expect(parsedModeToggle).toHaveAttribute('aria-checked', 'false');

			await page.getByRole('button', { name: 'Start', exact: true }).first().click();

			await expect(
				page.getByText('structured container log marker', { exact: true }).filter({ visible: true })
			).toBeVisible();
			await expect(parsedModeToggle).toHaveAttribute('aria-checked', 'true');
			await expect
				.poll(() => page.evaluate(() => localStorage.getItem('arcane_log_json_parsing_v3')))
				.toBe('false');
		});

		test('should show non-live fallback monitors on the logs tab for stopped containers', async ({
			page
		}) => {
			const stopped = containersData.data.find((c) => c.state !== 'running');
			expect(stopped, 'No stopped container available').toBeDefined();

			await page.goto(`/containers/${stopped!.id}`);
			await page.waitForLoadState('load');

			await page.getByRole('tab', { name: 'Logs' }).click();

			await expect(page.getByTestId('container-log-cpu-monitor')).toBeVisible();
			await expect(page.getByTestId('container-log-memory-monitor')).toBeVisible();
			await expect(page.getByTestId('container-log-cpu-monitor')).toContainText('N/A');
			await expect(page.getByTestId('container-log-memory-monitor')).toContainText('N/A');
		});

		test('downloads the full log history from the logs tab', async ({ page }, testInfo) => {
			const running = containersData.data.find((c) => c.state === 'running');
			expect(running, 'No running container available').toBeDefined();

			await page.goto(`/containers/${running!.id}`);
			await page.waitForLoadState('load');

			await page.getByRole('tab', { name: 'Logs' }).click();

			const downloadPromise = page.waitForEvent('download');
			await page
				.getByRole('button', { name: 'Download', exact: true })
				.filter({ visible: true })
				.click();
			const download = await downloadPromise;
			expect(download.suggestedFilename()).toBe(`container-${running!.id.slice(0, 12)}-logs.log`);
			const downloadPath = testInfo.outputPath(download.suggestedFilename());
			await download.saveAs(downloadPath);
			expect((await stat(downloadPath)).isFile()).toBe(true);
		});

		test('should show correct actions based on container state (without changing state)', async ({
			page
		}) => {
			const running = containersData.data.find((c) => c.state === 'running');
			const stopped = containersData.data.find((c) => c.state !== 'running');

			await navigateToContainers(page);

			expect(running, 'Running container fixture must exist').toBeDefined();
			expect(stopped, 'Stopped container fixture must exist').toBeDefined();
			const runningName = running!.names?.[0]?.replace(/^\/+/, '') ?? running!.id;
			const runningRow = page
				.getByRole('row')
				.filter({ has: page.getByRole('link', { name: runningName, exact: true }) });
			const runningMenu = await openRowActionsMenu(page, runningRow);
			await expect(
				runningMenu.getByRole('menuitem', { name: 'Restart', exact: true })
			).toBeVisible();
			await expect(runningMenu.getByRole('menuitem', { name: 'Stop', exact: true })).toBeVisible();
			await page.keyboard.press('Escape');

			const stoppedName = stopped!.names?.[0]?.replace(/^\/+/, '') ?? stopped!.id;
			const stoppedRow = page
				.getByRole('row')
				.filter({ has: page.getByRole('link', { name: stoppedName, exact: true }) });
			const stoppedMenu = await openRowActionsMenu(page, stoppedRow);
			await expect(stoppedMenu.getByRole('menuitem', { name: 'Start', exact: true })).toBeVisible();
			await page.keyboard.press('Escape');
		});

		test('should open the Remove dialog from row actions and allow cancel', async ({ page }) => {
			expect(containersData.data.length, 'No containers available').toBeGreaterThan(0);
			const container = containersData.data[0];

			await navigateToContainers(page);

			const containerName = container.names?.[0]?.replace(/^\/+/, '') ?? container.id;
			const row = page
				.getByRole('row')
				.filter({ has: page.getByRole('link', { name: containerName, exact: true }) });
			const menu = await openRowActionsMenu(page, row);
			await menu.getByRole('menuitem', { name: 'Remove', exact: true }).click();

			const dialog = page.getByRole('dialog');
			await expect(dialog).toBeVisible();
			await expect(
				dialog.getByRole('heading', { name: 'Confirm Container Removal', exact: true })
			).toBeVisible();

			await page.getByRole('button', { name: 'Cancel' }).click();
			await expect(dialog).toBeHidden();
		});
	});

	test.describe('Containers Page network IP addresses', () => {
		test('should show every network IP address for a multi-network container', async ({
			page,
			context
		}) => {
			await page.addInitScript(() => {
				localStorage.removeItem('arcane-container-table');
			});

			await context.route('**/api/environments/*/containers**', async (route) => {
				if (route.request().method() !== 'GET') {
					await route.continue();
					return;
				}

				const url = new URL(route.request().url());
				if (!/^\/api\/environments\/[^/]+\/containers$/.test(url.pathname)) {
					await route.continue();
					return;
				}

				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: [
							{
								id: 'wordpress-multi-network',
								names: ['/wordpress'],
								image: 'wordpress:latest',
								imageId: 'sha256:wordpress',
								command: 'apache2-foreground',
								created: 1_700_000_000,
								labels: {},
								state: 'running',
								status: 'Up 5 minutes',
								ports: [],
								hostConfig: { networkMode: 'default' },
								networkSettings: {
									networks: {
										proxy: { ipAddress: '172.20.0.10' },
										private: { ipAddress: '10.10.0.5' }
									}
								},
								mounts: []
							}
						],
						counts: {
							runningContainers: 1,
							stoppedContainers: 0,
							totalContainers: 1
						},
						pagination: {
							totalPages: 1,
							totalItems: 1,
							currentPage: 1,
							itemsPerPage: 20,
							grandTotalItems: 1
						}
					})
				});
			});

			await navigateToContainers(page);

			const row = page.getByRole('row').filter({
				has: page.getByRole('link', { name: 'wordpress', exact: true })
			});

			await expect(row).toContainText('10.10.0.5');
			await expect(row).toContainText('172.20.0.10');
		});
	});

	test.describe('Container form', () => {
		test('adds and removes container capabilities without retaining the previous selection', async ({
			page
		}) => {
			await page.goto('/containers/new');
			await page.getByRole('tab', { name: 'Advanced', exact: true }).click();

			const capAdd = page.getByText('Add capabilities', { exact: true }).locator('..');
			const selector = capAdd.getByRole('combobox');
			await selector.click();
			await page.getByRole('option', { name: 'NET_ADMIN', exact: true }).click();

			const badge = capAdd.locator('[data-slot="badge"]').filter({ hasText: 'NET_ADMIN' });
			await expect(badge).toBeVisible();
			await badge.getByRole('button').click();
			await expect(badge).toHaveCount(0);
			await expect(selector).toContainText('Select an option');
		});
	});
});

test.describe('Lifecycle', () => {
	const TEST_IMAGE = 'public.ecr.aws/docker/library/busybox:1.37';

	type CreatedContainer = {
		id: string;
		name: string;
		image: string;
		status: string;
	};

	type ContainerDetails = {
		id: string;
		name: string;
		image: string;
		activityId?: string;
		state: {
			status: string;
			running: boolean;
		};
		config: {
			env?: string[];
			cmd?: string[];
			workingDir?: string;
			healthcheck?: {
				test?: string[];
				interval?: number;
				timeout?: number;
				retries?: number;
			};
		};
		hostConfig: {
			restartPolicy?: string;
		};
		networkSettings: {
			networks: Record<string, { aliases?: string[] }>;
		};
		mounts: Array<{
			type: string;
			name?: string;
			source: string;
			destination: string;
		}>;
		ports: Array<{
			privatePort: number;
			publicPort?: number;
			type: string;
		}>;
		labels?: Record<string, string>;
	};

	type ActivityDetail = {
		activity: {
			id: string;
			type: string;
			status: string;
			error?: string;
		};
	};

	type ActionResult = {
		message?: string;
		activityId?: string;
	};

	type CommitResult = {
		id: string;
	};

	type CreatedProject = {
		id: string;
		name: string;
	};

	type ContainerCreatePayload = {
		name: string;
		image: string;
		cmd?: string[];
		workingDir?: string;
		env?: string[];
		labels?: Record<string, string>;
		healthcheck?: {
			test?: string[];
			interval?: number;
			timeout?: number;
			retries?: number;
		};
		hostConfig?: {
			binds?: string[];
			portBindings?: Record<string, Array<{ hostIp?: string; hostPort?: string }>>;
			restartPolicy?: { name: string; maximumRetryCount?: number };
		};
		networkingConfig?: {
			endpointsConfig?: Record<string, { aliases?: string[] }>;
		};
	};

	async function updateExperimentalFeatures(page: Page, enabled: string) {
		const response = await page.request.put('/api/environments/0/settings', {
			data: { experimentalFeaturesEnabled: enabled }
		});
		if (!response.ok()) {
			throw new Error(
				`Update experimentalFeaturesEnabled failed with ${response.status()}: ${await response.text()}`
			);
		}
		const settingsResponse = await page.request.get('/api/environments/0/settings');
		expect(settingsResponse.ok(), 'Read back experimental feature setting').toBe(true);
		const settings = (await settingsResponse.json()) as Array<{ key: string; value: string }>;
		expect(settings).toContainEqual(
			expect.objectContaining({ key: 'experimentalFeaturesEnabled', value: enabled })
		);
	}

	async function getContainer(page: Page, containerId: string): Promise<ContainerDetails> {
		return readApiData<ContainerDetails>(
			await page.request.get(`/api/environments/0/containers/${containerId}`),
			`Get container ${containerId}`
		);
	}

	async function expectContainerStatus(page: Page, containerId: string, status: string) {
		await expect
			.poll(async () => (await getContainer(page, containerId)).state.status, {
				message: `Expected container ${containerId} to reach ${status}`,
				timeout: 20_000
			})
			.toBe(status);
	}

	async function expectContainerMissing(page: Page, containerId: string) {
		await expect
			.poll(
				async () =>
					(await page.request.get(`/api/environments/0/containers/${containerId}`)).status(),
				{ message: `Expected container ${containerId} to be removed`, timeout: 20_000 }
			)
			.toBe(404);
	}

	async function expectActivitySucceeded(page: Page, activityId: string, expectedType: string) {
		await expect
			.poll(
				async () => {
					const detail = await readApiData<ActivityDetail>(
						await page.request.get(`/api/environments/0/activities/${activityId}`),
						`Get activity ${activityId}`
					);
					return detail.activity.status;
				},
				{ message: `Expected ${expectedType} activity ${activityId} to succeed`, timeout: 20_000 }
			)
			.toBe('success');
		const detail = await readApiData<ActivityDetail>(
			await page.request.get(`/api/environments/0/activities/${activityId}`),
			`Get completed activity ${activityId}`
		);
		expect(detail.activity.type).toBe(expectedType);
		expect(detail.activity.error).toBeFalsy();
	}

	// Header actions render inline or inside the "More actions" menu depending on placement.
	async function clickHeaderAction(page: Page, name: string) {
		const header = page.locator('[data-tabs-root] > :not([inert])');
		const inline = header
			.getByRole('button', { name, exact: true })
			.or(header.getByRole('link', { name, exact: true }))
			.filter({ visible: true })
			.first();
		const menuTrigger = header
			.getByRole('button', { name: 'More actions', exact: true })
			.filter({ visible: true })
			.first();
		const inlineVisible = await inline
			.waitFor({ state: 'visible', timeout: 5_000 })
			.then(() => true)
			.catch(() => false);
		if (inlineVisible) {
			await inline.click();
			return;
		}
		await menuTrigger.click();
		await page.getByRole('menuitem', { name, exact: true }).click();
	}

	async function runSimpleContainerAction(
		page: Page,
		containerId: string,
		pathAction: string,
		buttonName: string,
		expectedActivityType: string
	) {
		return test.step(`${buttonName} container`, async () => {
			const responseTimeout = pathAction === 'stop' ? 120_000 : 60_000;
			const matchesActionRequest = (url: string, method: string) => {
				const pathname = new URL(url).pathname.replace(/\/$/, '');
				return method === 'POST' && pathname.endsWith(`/containers/${containerId}/${pathAction}`);
			};
			const requestPromise = page.waitForRequest(
				(request) => matchesActionRequest(request.url(), request.method()),
				{ timeout: 30_000 }
			);
			const responsePromise = page.waitForResponse(
				(response) => matchesActionRequest(response.url(), response.request().method()),
				{ timeout: responseTimeout }
			);
			await clickHeaderAction(page, buttonName);
			await requestPromise;
			const result = await readApiData<ActionResult>(
				await responsePromise,
				`${buttonName} container ${containerId}`
			);
			expect(result.activityId, `${buttonName} must return an activity ID`).toBeTruthy();
			await expectActivitySucceeded(page, result.activityId!, expectedActivityType);
		});
	}

	async function selectSearchableOption(page: Page, scope: Locator, option: string) {
		await scope.getByRole('combobox').filter({ visible: true }).first().click();
		await page.getByRole('option', { name: option, exact: true }).click();
	}

	function formGroup(page: Page, heading: string) {
		return page
			.locator('div.space-y-4')
			.filter({ has: page.getByRole('heading', { name: heading, exact: true }) })
			.first();
	}

	async function createContainerThroughUI(
		page: Page,
		containerName: string,
		volumeName: string,
		networkName: string,
		containerIds: Set<string>
	) {
		return test.step('Create container', async () => {
			await page.goto('/containers/new');
			await page.getByRole('button', { name: 'Create Container', exact: true }).click();
			await expect(page.getByText('Container name is required', { exact: true })).toBeVisible();
			await expect(page.getByText('Image is required', { exact: true })).toBeVisible();

			await page.getByLabel('Container Name *', { exact: true }).fill(containerName);
			await page.getByLabel('Image *', { exact: true }).fill(TEST_IMAGE);
			await page.getByLabel('Command', { exact: true }).fill('sleep 3600');
			await page.getByLabel('Working Directory', { exact: true }).fill('/tmp');

			await page.getByRole('tab', { name: 'Environment', exact: true }).click();
			const environmentGroup = formGroup(page, 'Environment Variables');
			await environmentGroup.getByRole('button', { name: 'Add', exact: true }).click();
			await environmentGroup.getByPlaceholder('KEY').fill('E2E_VALUE');
			await environmentGroup.getByPlaceholder('value').fill('initial');

			const labelsGroup = formGroup(page, 'Labels');
			await labelsGroup.getByRole('button', { name: 'Add', exact: true }).click();
			await labelsGroup.getByPlaceholder('com.example.key').fill('arcane.e2e');
			await labelsGroup.getByPlaceholder('value').fill('container-lifecycle');

			await page.getByRole('tab', { name: 'Ports', exact: true }).click();
			await page.getByRole('button', { name: 'Add', exact: true }).click();
			await page.getByPlaceholder('0.0.0.0').fill('127.0.0.1');
			await page.getByPlaceholder('80', { exact: true }).fill('8080');

			await page.getByRole('tab', { name: 'Volumes', exact: true }).click();
			await page.getByRole('button', { name: 'Add Volume Mount', exact: true }).click();
			await selectSearchableOption(page, page.getByRole('tabpanel'), volumeName);
			await page.getByPlaceholder('Container path').fill('/data');

			await page.getByRole('tab', { name: 'Networks', exact: true }).click();
			await page.getByRole('button', { name: 'Add', exact: true }).click();
			await selectSearchableOption(page, page.getByRole('tabpanel'), networkName);
			await page.getByPlaceholder('Aliases').fill('lifecycle-alias');

			await page.getByRole('tab', { name: 'Advanced', exact: true }).click();
			await page.locator('#restart-policy').click();
			await page.getByRole('option', { name: 'On Failure', exact: true }).click();
			await page.getByLabel('Maximum Retries', { exact: true }).fill('3');
			await page.locator('#health-mode').click();
			await page.getByRole('option', { name: 'Custom', exact: true }).click();
			await page.getByLabel('Test Command', { exact: true }).fill('test "$E2E_VALUE" = "initial"');
			await page.getByLabel('Interval (s)', { exact: true }).fill('2');
			await page.getByLabel('Timeout (s)', { exact: true }).fill('1');
			await page.getByLabel('Retries', { exact: true }).fill('2');

			const requestPromise = page.waitForRequest(
				(request) =>
					request.method() === 'POST' &&
					new URL(request.url()).pathname === '/api/environments/0/containers'
			);
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/environments/0/containers'
			);
			await page.getByRole('button', { name: 'Create Container', exact: true }).click();

			const request = await requestPromise;
			const payload = request.postDataJSON() as ContainerCreatePayload;
			const created = await readApiData<CreatedContainer>(
				await responsePromise,
				`Create container ${containerName}`
			);
			containerIds.add(created.id);
			await expect(page).toHaveURL((url) => url.pathname === `/containers/${created.id}`);

			expect(payload).toMatchObject({
				name: containerName,
				image: TEST_IMAGE,
				cmd: ['sleep', '3600'],
				workingDir: '/tmp',
				env: ['E2E_VALUE=initial'],
				labels: { 'arcane.e2e': 'container-lifecycle' },
				healthcheck: {
					test: ['CMD-SHELL', 'test "$E2E_VALUE" = "initial"'],
					interval: 2,
					timeout: 1,
					retries: 2
				},
				hostConfig: {
					binds: [`${volumeName}:/data`],
					portBindings: {
						'8080/tcp': [{ hostIp: '127.0.0.1', hostPort: '' }]
					},
					restartPolicy: { name: 'on-failure', maximumRetryCount: 3 }
				},
				networkingConfig: {
					endpointsConfig: {
						[networkName]: { aliases: ['lifecycle-alias'] }
					}
				}
			});

			return created;
		});
	}

	async function verifyShell(page: Page, marker: string) {
		return test.step('Verify shell and volume', async () => {
			await page.getByRole('tab', { name: 'Shell', exact: true }).click();
			await expect(page.getByText('Live', { exact: true })).toBeVisible({ timeout: 15_000 });

			const terminal = page.locator('.terminal-container');
			const input = terminal.locator('textarea.xterm-helper-textarea');
			await expect(input).toBeAttached();
			await input.pressSequentially(
				`echo ${marker}-$E2E_VALUE && echo volume-ok > /data/e2e-marker && cat /data/e2e-marker`,
				{ delay: 5 }
			);
			await input.press('Enter');
			await expect(terminal.locator('.xterm-rows')).toContainText(`${marker}-initial`, {
				timeout: 15_000
			});
			await expect(terminal.locator('.xterm-rows')).toContainText('volume-ok');
		});
	}

	async function connectAndDisconnectNetwork(page: Page, containerId: string, networkName: string) {
		return test.step('Connect and disconnect network', async () => {
			await page.getByRole('tab', { name: 'Networks', exact: true }).click();
			const connectSection = page.locator('div').filter({
				has: page.getByRole('heading', { name: 'Connect to network', exact: true })
			});
			await selectSearchableOption(page, connectSection, networkName);
			await connectSection.getByPlaceholder('Aliases').fill('attached-live');

			const connectResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					/\/api\/environments\/0\/networks\/[^/]+\/connect$/.test(new URL(response.url()).pathname)
			);
			await connectSection.getByRole('button', { name: 'Connect', exact: true }).click();
			expect((await connectResponsePromise).ok()).toBe(true);
			await expect
				.poll(async () =>
					Object.keys((await getContainer(page, containerId)).networkSettings.networks)
				)
				.toContain(networkName);

			const networkCard = page
				.locator('[data-slot="card"]')
				.filter({ has: page.getByText(networkName, { exact: true }) })
				.filter({ has: page.getByRole('button', { name: 'Disconnect', exact: true }) })
				.last();
			await networkCard.getByRole('button', { name: 'Disconnect', exact: true }).click();
			const disconnectResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					/\/api\/environments\/0\/networks\/[^/]+\/disconnect$/.test(
						new URL(response.url()).pathname
					)
			);
			await page
				.getByRole('dialog')
				.getByRole('button', { name: 'Disconnect', exact: true })
				.click();
			expect((await disconnectResponsePromise).ok()).toBe(true);
			await expect(page.getByRole('dialog')).toBeHidden();
			await expect(page.locator('[data-dialog-overlay]')).toHaveCount(0);
			await expect
				.poll(async () =>
					Object.keys((await getContainer(page, containerId)).networkSettings.networks)
				)
				.not.toContain(networkName);
		});
	}

	async function commitContainer(
		page: Page,
		containerId: string,
		containerName: string,
		repository: string,
		imageIds: Set<string>
	) {
		return test.step('Commit image', async () => {
			await clickHeaderAction(page, 'Commit');
			const dialog = page.getByRole('dialog', { name: `Commit "${containerName}"` });
			await waitForDialogReady(dialog);
			await expect(dialog.locator('#repository')).toBeFocused();
			await dialog.locator('#repository').fill(repository);
			await dialog.locator('#tag').fill('e2e');
			await dialog.getByLabel('Description', { exact: true }).fill('Playwright lifecycle snapshot');
			await dialog.getByLabel('Author', { exact: true }).fill('Arcane Playwright');
			await expect(dialog.locator('#repository')).toHaveValue(repository);
			await expect(dialog.locator('#tag')).toHaveValue('e2e');
			await expect(dialog.getByLabel('Description', { exact: true })).toHaveValue(
				'Playwright lifecycle snapshot'
			);
			await expect(dialog.getByLabel('Author', { exact: true })).toHaveValue('Arcane Playwright');

			const requestPromise = page.waitForRequest(
				(request) =>
					request.method() === 'POST' &&
					new URL(request.url()).pathname === `/api/environments/0/containers/${containerId}/commit`
			);
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname ===
						`/api/environments/0/containers/${containerId}/commit`
			);
			await dialog.getByRole('button', { name: 'Commit', exact: true }).click();
			const request = await requestPromise;
			const result = await readApiData<CommitResult>(
				await responsePromise,
				`Commit container ${containerId}`
			);
			imageIds.add(result.id);
			expect(request.postDataJSON()).toMatchObject({
				repository,
				tag: 'e2e',
				comment: 'Playwright lifecycle snapshot',
				author: 'Arcane Playwright'
			});
			expect(result.id).toMatch(/^sha256:/);
			await expect(dialog).toBeHidden();
			return result;
		});
	}

	async function editContainerThroughUI(
		page: Page,
		containerId: string,
		newContainerName: string,
		containerIds: Set<string>
	) {
		return test.step('Edit and recreate container', async () => {
			await clickHeaderAction(page, 'Edit');
			await expect(page).toHaveURL(`/containers/${containerId}/edit`);
			await expect(page.getByText('Applying changes recreates this container.')).toBeVisible();
			await expect(page.getByLabel('Container Name *', { exact: true })).toHaveValue(
				newContainerName.replace(/-edited$/, '')
			);
			await expect(page.getByLabel('Command', { exact: true })).toHaveValue('sleep 3600');

			await page.getByLabel('Container Name *', { exact: true }).fill(newContainerName);
			await page.getByLabel('Command', { exact: true }).fill('sleep 7200');
			await page.getByRole('tab', { name: 'Environment', exact: true }).click();
			const environmentGroup = formGroup(page, 'Environment Variables');
			const environmentKeys = environmentGroup.getByPlaceholder('KEY');
			const customEnvironmentIndex = await environmentKeys.evaluateAll((inputs) =>
				inputs.findIndex((input) => (input as HTMLInputElement).value === 'E2E_VALUE')
			);
			expect(customEnvironmentIndex).toBeGreaterThanOrEqual(0);
			const customEnvironmentRow = environmentKeys.nth(customEnvironmentIndex).locator('..');
			await customEnvironmentRow.getByPlaceholder('value').fill('edited');

			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === `/api/environments/0/containers/${containerId}/edit`,
				{ timeout: 60_000 }
			);
			await page.getByRole('button', { name: 'Save', exact: true }).click();
			const dialog = page.getByRole('dialog', { name: 'Recreate container?' });
			await dialog.getByRole('button', { name: 'Save', exact: true }).click();
			const edited = await readApiData<ContainerDetails>(
				await responsePromise,
				`Edit container ${containerId}`
			);
			containerIds.add(edited.id);
			await expect(page).toHaveURL((url) => url.pathname === `/containers/${edited.id}`);
			expect(edited.id).not.toBe(containerId);
			expect(edited.activityId).toBeTruthy();
			await expectActivitySucceeded(page, edited.activityId!, 'container_edit');
			return edited;
		});
	}

	async function redeployContainerThroughUI(
		page: Page,
		containerId: string,
		containerIds: Set<string>
	) {
		return test.step('Redeploy container', async () => {
			await clickHeaderAction(page, 'Redeploy');
			const dialog = page.getByRole('dialog');
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname ===
						`/api/environments/0/containers/${containerId}/redeploy`,
				{ timeout: 60_000 }
			);
			await dialog.getByRole('button', { name: 'Redeploy', exact: true }).click();
			const redeployed = await readApiData<ContainerDetails>(
				await responsePromise,
				`Redeploy container ${containerId}`
			);
			containerIds.add(redeployed.id);
			await expect(page).toHaveURL((url) => url.pathname === `/containers/${redeployed.id}`);
			expect(redeployed.id).not.toBe(containerId);
			expect(redeployed.activityId).toBeTruthy();
			await expectActivitySucceeded(page, redeployed.activityId!, 'container_redeploy');
			return redeployed;
		});
	}

	async function convertContainerToProject(
		page: Page,
		containerId: string,
		containerName: string,
		projectIds: Set<string>
	) {
		return test.step('Convert to Compose', async () => {
			const generateResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/environments/0/containers/generate-compose'
			);
			await clickHeaderAction(page, 'Convert to Compose');
			const generated = await readApiData<{ composeContent: string }>(
				await generateResponsePromise,
				`Generate Compose for ${containerId}`
			);
			expect(generated.composeContent).toContain(containerName);
			await expect(page).toHaveURL(`/projects/new?fromContainers=${containerId}&fromEnv=0`);
			await expect(page.locator('.cm-editor').first()).toContainText(containerName);

			const createButton = page.locator('button[data-action="create"]');
			await expect(createButton).toBeEnabled({ timeout: 15_000 });
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/environments/0/projects'
			);
			await createButton.click();
			const dialog = page.getByRole('dialog');
			await expect(
				dialog.getByLabel('Remove original container(s) after creation')
			).not.toBeChecked();
			await dialog.getByRole('button', { name: 'Create Project', exact: true }).click();
			const project = await readApiData<CreatedProject>(
				await responsePromise,
				`Create converted project for ${containerId}`
			);
			projectIds.add(project.id);
			await expect(page).toHaveURL((url) => url.pathname === `/projects/${project.id}`);
			return project;
		});
	}

	async function killContainerThroughUI(page: Page, containerId: string, containerName: string) {
		return test.step('Kill container', async () => {
			await clickHeaderAction(page, 'Kill');
			const dialog = page.getByRole('dialog', { name: `Kill "${containerName}"` });
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === `/api/environments/0/containers/${containerId}/kill`
			);
			await dialog.getByRole('button', { name: 'Kill container', exact: true }).click();
			const result = await readApiData<ActionResult>(
				await responsePromise,
				`Kill container ${containerId}`
			);
			expect(result.activityId).toBeTruthy();
			await expectActivitySucceeded(page, result.activityId!, 'container_kill');
		});
	}

	async function removeContainerThroughUI(page: Page, containerId: string) {
		return test.step('Remove container', async () => {
			await clickHeaderAction(page, 'Remove');
			const responsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'DELETE' &&
					new URL(response.url()).pathname === `/api/environments/0/containers/${containerId}`
			);
			await page.getByRole('dialog').getByRole('button', { name: 'Remove', exact: true }).click();
			const result = await readApiData<ActionResult>(
				await responsePromise,
				`Remove container ${containerId}`
			);
			expect(result.activityId).toBeTruthy();
			await expectActivitySucceeded(page, result.activityId!, 'container_delete');
			await expect(page).toHaveURL('/containers');
		});
	}

	test('creates, mutates, converts, and removes a standalone container', async ({
		page,
		registerCleanup
	}) => {
		test.setTimeout(300_000);
		page.setDefaultTimeout(15_000);
		page.setDefaultNavigationTimeout(20_000);

		const suffix = Date.now().toString(36);
		const containerName = `e2e-container-${suffix}`;
		const editedContainerName = `${containerName}-edited`;
		const volumeName = `e2e-volume-${suffix}`;
		const primaryNetworkName = `e2e-network-primary-${suffix}`;
		const secondaryNetworkName = `e2e-network-secondary-${suffix}`;
		const committedRepository = `arcane-e2e/${containerName}`;
		let currentContainerId: string | null = null;
		let committedImageId: string | null = null;
		const containerIds = new Set<string>();
		const imageIds = new Set<string>();
		const projectIds = new Set<string>();
		let settingsChanged = false;
		let originalExperimentalSetting = 'false';

		registerCleanup(async () => {
			for (const id of projectIds)
				await removeApiResource(page, `/api/environments/0/projects/${id}/destroy`, {
					data: { removeVolumes: false }
				});
			for (const id of containerIds)
				await removeApiResource(
					page,
					`/api/environments/0/containers/${id}?force=true&volumes=false`
				);
			for (const id of imageIds)
				await removeApiResource(
					page,
					`/api/environments/0/images/${encodeURIComponent(id)}?force=true`
				);
			for (const name of [secondaryNetworkName, primaryNetworkName])
				await removeApiResource(page, `/api/environments/0/networks/${encodeURIComponent(name)}`);
			await removeApiResource(
				page,
				`/api/environments/0/volumes/${encodeURIComponent(volumeName)}?force=true`
			);
			if (settingsChanged) await updateExperimentalFeatures(page, originalExperimentalSetting);
		});

		const settingsResponse = await page.request.get('/api/environments/0/settings');
		if (!settingsResponse.ok()) {
			throw new Error(`Get settings failed with ${settingsResponse.status()}`);
		}
		const settings = (await settingsResponse.json()) as Array<{ key: string; value: string }>;
		const experimentalSetting = settings.find(
			(setting) => setting.key === 'experimentalFeaturesEnabled'
		);
		expect(experimentalSetting, 'Experimental feature setting must be present').toBeDefined();
		originalExperimentalSetting = experimentalSetting!.value;
		settingsChanged = true;
		await updateExperimentalFeatures(page, 'true');

		await readApiData(
			await page.request.post('/api/environments/0/volumes', {
				data: { name: volumeName, driver: 'local' }
			}),
			`Create volume ${volumeName}`
		);
		for (const networkName of [primaryNetworkName, secondaryNetworkName]) {
			await readApiData(
				await page.request.post('/api/environments/0/networks', {
					data: { name: networkName, options: { driver: 'bridge' } }
				}),
				`Create network ${networkName}`
			);
		}

		const created = await createContainerThroughUI(
			page,
			containerName,
			volumeName,
			primaryNetworkName,
			containerIds
		);
		currentContainerId = created.id;

		let details = await getContainer(page, currentContainerId);
		expect(details.name).toBe(containerName);
		expect(details.image).toBe(TEST_IMAGE);
		expect(details.state.status).toBe('running');
		expect(details.config.cmd).toEqual(['sleep', '3600']);
		expect(details.config.workingDir).toBe('/tmp');
		expect(details.config.env).toContain('E2E_VALUE=initial');
		expect(details.config.healthcheck?.test).toEqual([
			'CMD-SHELL',
			'test "$E2E_VALUE" = "initial"'
		]);
		expect(details.config.healthcheck?.interval).toBe(2_000_000_000);
		expect(details.hostConfig.restartPolicy).toBe('on-failure');
		expect(details.labels?.['arcane.e2e']).toBe('container-lifecycle');
		expect(details.ports.some((port) => port.privatePort === 8080 && port.type === 'tcp')).toBe(
			true
		);
		expect(details.mounts).toEqual(
			expect.arrayContaining([
				expect.objectContaining({ type: 'volume', name: volumeName, destination: '/data' })
			])
		);
		expect(details.networkSettings.networks[primaryNetworkName]?.aliases).toContain(
			'lifecycle-alias'
		);

		await verifyShell(page, `shell-${suffix}`);
		await page.getByRole('tab', { name: 'Overview', exact: true }).click();

		await runSimpleContainerAction(page, currentContainerId, 'pause', 'Pause', 'container_pause');
		await expectContainerStatus(page, currentContainerId, 'paused');
		await runSimpleContainerAction(
			page,
			currentContainerId,
			'unpause',
			'Unpause',
			'container_unpause'
		);
		await expectContainerStatus(page, currentContainerId, 'running');
		await runSimpleContainerAction(
			page,
			currentContainerId,
			'restart',
			'Restart',
			'container_restart'
		);
		await expectContainerStatus(page, currentContainerId, 'running');
		await runSimpleContainerAction(page, currentContainerId, 'stop', 'Stop', 'container_stop');
		await expectContainerStatus(page, currentContainerId, 'exited');
		await runSimpleContainerAction(page, currentContainerId, 'start', 'Start', 'container_start');
		await expectContainerStatus(page, currentContainerId, 'running');

		await connectAndDisconnectNetwork(page, currentContainerId, secondaryNetworkName);

		const commit = await commitContainer(
			page,
			currentContainerId,
			containerName,
			committedRepository,
			imageIds
		);
		committedImageId = commit.id;
		const committedImageResponse = await page.request.get(
			`/api/environments/0/images/${encodeURIComponent(committedImageId)}`
		);
		expect(committedImageResponse.ok()).toBe(true);

		const oldContainerId = currentContainerId;
		const edited = await editContainerThroughUI(
			page,
			currentContainerId,
			editedContainerName,
			containerIds
		);
		currentContainerId = edited.id;
		await expectContainerMissing(page, oldContainerId);
		details = await getContainer(page, currentContainerId);
		expect(details.name).toBe(editedContainerName);
		expect(details.config.cmd).toEqual(['sleep', '7200']);
		expect(details.config.env).toContain('E2E_VALUE=edited');
		expect(details.mounts.some((mount) => mount.name === volumeName)).toBe(true);
		expect(details.ports.some((port) => port.privatePort === 8080)).toBe(true);
		expect(details.networkSettings.networks).toHaveProperty(primaryNetworkName);

		const preRedeployId = currentContainerId;
		const redeployed = await redeployContainerThroughUI(page, currentContainerId, containerIds);
		currentContainerId = redeployed.id;
		await expectContainerMissing(page, preRedeployId);
		await expectContainerStatus(page, currentContainerId, 'running');

		const convertedProject = await convertContainerToProject(
			page,
			currentContainerId,
			editedContainerName,
			projectIds
		);
		expect(projectIds.has(convertedProject.id)).toBe(true);
		expect(
			(await page.request.get(`/api/environments/0/containers/${currentContainerId}`)).ok()
		).toBe(true);

		await page.goto(`/containers/${currentContainerId}`);
		await killContainerThroughUI(page, currentContainerId, editedContainerName);
		await expectContainerStatus(page, currentContainerId, 'exited');
		await runSimpleContainerAction(page, currentContainerId, 'start', 'Start', 'container_start');
		await expectContainerStatus(page, currentContainerId, 'running');
		await runSimpleContainerAction(page, currentContainerId, 'stop', 'Stop', 'container_stop');
		await expectContainerStatus(page, currentContainerId, 'exited');

		await removeContainerThroughUI(page, currentContainerId);
		await expectContainerMissing(page, currentContainerId);
		currentContainerId = null;
	});
});

test.describe('Resource sorting', () => {
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
				return sort === 'cpuUsage'
					? c.resourceSample.cpuPercent
					: c.resourceSample.memoryUsageBytes;
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
		await page.clock.install();
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
		await page.clock.runFor(6500);
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
});

test.describe('Project grouping', () => {
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

	test('grouped containers do not split the same project across pages', async ({
		page,
		context
	}) => {
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
					groupedMockPayload?.groups.find((group) => group.groupName === 'immich')?.items.length ??
					0
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
		return page
			.locator('table tbody tr')
			.filter({ has: page.getByText(groupName, { exact: true }) });
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
});
