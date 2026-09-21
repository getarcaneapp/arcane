import { expect, test, type Locator, type Page, type Response } from '../fixtures/test.fixture';
import { readApiData, removeApiResource } from '../utils/fetch.util';
import { waitForDialogReady } from '../utils/playwright.util';

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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('button', { name: buttonName, exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
		await page.getByRole('dialog').getByRole('button', { name: 'Disconnect', exact: true }).click();
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('button', { name: 'Commit', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
				new URL(response.url()).pathname === `/api/environments/0/containers/${containerId}/commit`
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('link', { name: 'Edit', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('button', { name: 'Redeploy', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('link', { name: 'Convert to Compose', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('button', { name: 'Kill', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
		await page
			.locator('[data-tabs-root] > :not([inert])')
			.getByRole('button', { name: 'Remove', exact: true })
			.filter({ visible: true })
			.first()
			.click();
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
	expect(details.config.healthcheck?.test).toEqual(['CMD-SHELL', 'test "$E2E_VALUE" = "initial"']);
	expect(details.config.healthcheck?.interval).toBe(2_000_000_000);
	expect(details.hostConfig.restartPolicy).toBe('on-failure');
	expect(details.labels?.['arcane.e2e']).toBe('container-lifecycle');
	expect(details.ports.some((port) => port.privatePort === 8080 && port.type === 'tcp')).toBe(true);
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
