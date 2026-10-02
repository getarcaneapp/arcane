import { waitForDialogReady } from '../utils/playwright.util';
import { removeCreatedEnvironments, readApiData } from '../utils/fetch.util';
import {
	test,
	expect,
	type Locator,
	type Page,
	type Request,
	type Route
} from '../fixtures/test.fixture';

test.describe('Settings', () => {
	const LOCAL_ENV_ID = '0';
	const NAME_PLACEHOLDER = 'My Lab Server';

	async function openEnvironment(page: Page, environmentId: string) {
		await page.goto(`/environments/${environmentId}`);
		await page.waitForLoadState('load');
		await expect(environmentTitleButton(page)).toBeVisible();
		await expect(page.getByRole('button', { name: 'Save', exact: true }).first()).toBeVisible();
	}

	function environmentTitleButton(page: Page) {
		return page.locator('h1 button').first();
	}

	async function renameEnvironmentInHeader(page: Page, newName: string) {
		await environmentTitleButton(page).click();
		const nameInput = page.getByPlaceholder(NAME_PLACEHOLDER);
		await expect(nameInput).toBeVisible();
		await nameInput.fill(newName);
		await nameInput.press('Enter');
		await expect(environmentTitleButton(page)).toHaveText(newName);
	}

	async function createDirectEnvironmentViaUI(
		page: Page,
		environmentName: string,
		environmentIds: Set<string>
	) {
		await page.goto('/environments');
		await page.waitForLoadState('load');

		await page.getByRole('button', { name: 'Add Environment', exact: true }).click();
		await expect(page.getByText('Create New Agent Environment')).toBeVisible();

		const dialog = page.getByRole('dialog');
		await waitForDialogReady(dialog);
		await dialog.getByLabel('Name', { exact: true }).fill(environmentName);
		await dialog.getByLabel('Agent Address', { exact: true }).fill('localhost:3552');
		const createResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === '/api/environments'
		);
		await page.getByRole('button', { name: 'Generate Agent Configuration', exact: true }).click();
		const createResponse = await createResponsePromise;
		expect(createResponse.ok(), 'Create direct environment').toBeTruthy();
		const created: { data: { id: string; apiKey?: string } } = await createResponse.json();
		environmentIds.add(created.data.id);
		expect(created.data.id).toBeTruthy();
		if (!created.data.apiKey) throw new Error('New agent environment did not return an API key');

		await expect(
			page.getByRole('heading', { name: 'Environment Created Successfully', exact: true })
		).toBeVisible();
		await page.getByRole('button', { name: 'Done', exact: true }).click();
		await expect(page.getByRole('button', { name: environmentName, exact: true })).toBeVisible();
		return { id: created.data.id, apiKey: created.data.apiKey };
	}

	async function openLocalEnvironment(page: Page) {
		await openEnvironment(page, LOCAL_ENV_ID);
	}

	async function saveAndWaitForPut(page: Page, expectedPath: string) {
		const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
		await expect(saveButton).toBeEnabled();

		const responsePromise = page.waitForResponse((response) => {
			const request = response.request();
			if (request.method() !== 'PUT') return false;
			const url = new URL(response.url());
			return url.pathname === expectedPath;
		});

		await saveButton.click();
		const response = await responsePromise;
		expect(response.ok(), `Expected successful PUT to ${expectedPath}`).toBeTruthy();
		await expect(saveButton).toBeDisabled({ timeout: 10000 });
		return response.request();
	}

	async function selectSettingOption(page: Page, trigger: Locator, optionText: string) {
		await expect(trigger).toBeVisible();
		await trigger.click();
		const option = page.getByRole('option').filter({ hasText: optionText }).first();
		await expect(option).toBeVisible();
		await option.click();
	}

	test.describe('Environment Settings UI', () => {
		test.describe.configure({ mode: 'serial' });
		const settingKeys = new Set([
			'baseServerUrl',
			'followProjectSymlinks',
			'defaultDeployPullPolicy',
			'trivyNetwork',
			'trivyResourceLimitsEnabled',
			'trivyMemoryLimitMb',
			'trivyCpuLimit'
		]);
		let originalSettings: Record<string, string> = {};
		test.beforeEach(async ({ page }) => {
			originalSettings = {};
			const response = await page.request.get(`/api/environments/${LOCAL_ENV_ID}/settings`);
			expect(response.ok(), 'Read original environment settings').toBe(true);
			const settings: Array<{ key: string; value: string }> = await response.json();
			expect(Array.isArray(settings)).toBe(true);
			originalSettings = Object.fromEntries(
				settings
					.filter((setting) => settingKeys.has(setting.key))
					.map((setting) => [setting.key, setting.value])
			);
		});
		test.afterEach(async ({ page }) => {
			if (Object.keys(originalSettings).length === 0) return;
			try {
				const currentResponse = await page.request.get(
					`/api/environments/${LOCAL_ENV_ID}/settings`
				);
				expect(currentResponse.ok()).toBe(true);
				const current: Array<{ key: string; value: string }> = await currentResponse.json();
				const changed = Object.fromEntries(
					Object.entries(originalSettings).filter(
						([key, value]) => current.find((setting) => setting.key === key)?.value !== value
					)
				);
				if (Object.keys(changed).length === 0) return;
				const response = await page.request.put(`/api/environments/${LOCAL_ENV_ID}/settings`, {
					data: changed
				});
				expect(
					response.ok(),
					`Restore environment settings: ${response.status()} ${await response.text()}`
				).toBe(true);
				const restoredResponse = await page.request.get(
					`/api/environments/${LOCAL_ENV_ID}/settings`
				);
				expect(restoredResponse.ok()).toBe(true);
				const restored: Array<{ key: string; value: string }> = await restoredResponse.json();
				expect(
					Object.fromEntries(
						restored
							.filter((setting) => settingKeys.has(setting.key))
							.map((setting) => [setting.key, setting.value])
					)
				).toEqual(originalSettings);
			} catch (error) {
				expect.soft(false, `Restore environment settings: ${String(error)}`).toBe(true);
			}
		});

		test('should keep primary tabs selectable and restore them from the URL', async ({ page }) => {
			await page.goto('/settings');
			await page.waitForLoadState('load');

			const jobScheduleCategory = page
				.getByRole('button')
				.filter({ hasText: 'Automations' })
				.first();
			await expect(jobScheduleCategory).toBeVisible();
			await jobScheduleCategory.click();

			await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('jobs');
			await expect(page.getByRole('tab', { name: 'Automations', exact: true })).toHaveAttribute(
				'data-state',
				'active'
			);

			const storageTab = page.getByRole('tab', { name: 'Storage & Limits', exact: true });
			await storageTab.click();
			await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('storage');
			await expect(storageTab).toHaveAttribute('data-state', 'active');

			const dockerTab = page.getByRole('tab', { name: 'Docker Settings', exact: true });
			await dockerTab.click();
			await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('docker');
			await page.reload();
			await expect(dockerTab).toHaveAttribute('data-state', 'active');

			await page.goto(`/environments/${LOCAL_ENV_ID}?source=e2e&tab=invalid#tab-state`);
			await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('features');
			const canonicalUrl = new URL(page.url());
			expect(canonicalUrl.searchParams.get('source')).toBe('e2e');
			expect(canonicalUrl.hash).toBe('#tab-state');
			await expect(page.getByRole('tab', { name: 'Features', exact: true })).toHaveAttribute(
				'data-state',
				'active'
			);
			await expect(page.getByRole('heading', { name: 'Features', exact: true })).toBeVisible();
			await expect(
				page.getByRole('switch', { name: 'Vulnerability management', exact: true })
			).toBeVisible();
		});

		test('should update and save environment details', async ({ page, registerCleanup }) => {
			test.setTimeout(120_000); // 120 seconds timeout for this lengthy UI workflow
			const envName = `settings-ui-${Date.now().toString().slice(-5)}`;
			const updatedName = `${envName}-updated`;
			const activityStatuses = new Map<string, number>();
			page.on('response', (response) => {
				const match = new URL(response.url()).pathname.match(
					/^\/api\/environments\/([^/]+)\/activities$/
				);
				if (match) activityStatuses.set(match[1], response.status());
			});
			const environmentIds = new Set<string>();

			registerCleanup(() => removeCreatedEnvironments(page, environmentIds, envName));
			const { id: environmentId, apiKey } = await createDirectEnvironmentViaUI(
				page,
				envName,
				environmentIds
			);
			const environmentPath = `/api/environments/${environmentId}`;
			await expect.poll(() => activityStatuses.get(environmentId)).toBe(502);
			expect((await page.request.get('/api/auth/me', { timeout: 5_000 })).status()).toBe(200);
			await expect(page).toHaveURL('/environments');
			await page.getByRole('button', { name: 'Open activity center' }).first().click();
			const activityCenter = page.getByRole('dialog', { name: 'Activity Center' });
			await expect(
				activityCenter.getByText(`Could not load activity from ${envName}`, { exact: true })
			).toBeVisible();
			await page.keyboard.press('Escape');
			await expect(activityCenter).toBeHidden();
			await page.getByRole('button', { name: envName, exact: true }).click();
			await expect(page).toHaveURL(/\/environments\/[^/?]+\?tab=[a-z]+$/);

			const apiUrlButton = page.getByTitle('API URL', { exact: true });
			const originalApiUrl = (await apiUrlButton.textContent())!.trim();
			const updatedApiUrl = originalApiUrl.replace(':3552', ':3553');
			expect(updatedApiUrl).not.toBe(originalApiUrl);

			await renameEnvironmentInHeader(page, updatedName);
			const nameRequest = await saveAndWaitForPut(page, environmentPath);
			expect(nameRequest.postDataJSON()).not.toHaveProperty('accessToken');

			await page.reload();
			await expect(environmentTitleButton(page)).toHaveText(updatedName);

			await apiUrlButton.click();
			await page.locator('#api-url').fill(updatedApiUrl);
			await page.locator('#api-url').press('Tab');
			const tokenInput = page.getByLabel('Agent access token', { exact: true });
			await expect(tokenInput).toBeVisible();
			await expect(tokenInput).toHaveAttribute('type', 'password');
			await expect(tokenInput).toHaveValue('');

			const rejectedResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'PUT' &&
					new URL(response.url()).pathname === environmentPath
			);
			await page.getByRole('button', { name: 'Save', exact: true }).first().click();
			const rejectedResponse = await rejectedResponsePromise;
			expect(rejectedResponse.status()).toBe(400);
			expect(rejectedResponse.request().postDataJSON()).not.toHaveProperty('accessToken');
			await expect(
				page.getByText('Changing environment API URL requires re-entering the accessToken')
			).toBeVisible();
			const afterRejection = await page.request.get(environmentPath);
			expect(afterRejection.ok()).toBe(true);
			expect(((await afterRejection.json()) as { data: { apiUrl: string } }).data.apiUrl).toBe(
				originalApiUrl
			);

			await tokenInput.fill(apiKey);
			await apiUrlButton.click();
			await page.locator('#api-url').fill(`${updatedApiUrl}/changed`);
			await page.locator('#api-url').press('Tab');
			await expect(tokenInput).toHaveValue('');
			await apiUrlButton.click();
			await page.locator('#api-url').fill(updatedApiUrl);
			await page.locator('#api-url').press('Tab');
			await tokenInput.fill(apiKey);
			await page.route(`**${environmentPath}`, async (route) => {
				if (route.request().method() !== 'PUT') return route.continue();
				await route.fulfill({
					status: 503,
					contentType: 'application/problem+json',
					body: JSON.stringify({ detail: 'Temporary update failure' })
				});
			});
			const failedResponsePromise = page.waitForResponse(
				(response) =>
					response.request().method() === 'PUT' &&
					new URL(response.url()).pathname === environmentPath
			);
			await page.getByRole('button', { name: 'Save', exact: true }).first().click();
			expect((await failedResponsePromise).status()).toBe(503);
			await expect(page.getByText('Temporary update failure')).toBeVisible();
			expect((await tokenInput.inputValue()) === apiKey).toBe(true);
			await page.unroute(`**${environmentPath}`);

			const updateRequest = await saveAndWaitForPut(page, environmentPath);
			const updatePayload = updateRequest.postDataJSON() as Record<string, unknown>;
			expect(updatePayload.apiUrl).toBe(updatedApiUrl);
			expect(updatePayload.accessToken === apiKey).toBe(true);
			await expect(tokenInput).toBeHidden();
			await apiUrlButton.click();
			await page.locator('#api-url').fill(`${updatedApiUrl}/another`);
			await page.locator('#api-url').press('Tab');
			await expect(tokenInput).toHaveValue('');
			await apiUrlButton.click();
			await page.locator('#api-url').press('Escape');
			await expect(apiUrlButton).toHaveText(updatedApiUrl);
			await expect(tokenInput).toBeHidden();

			await page.reload();
			await expect(apiUrlButton).toHaveText(updatedApiUrl);
			await expect(tokenInput).toBeHidden();
			await renameEnvironmentInHeader(page, `${updatedName}-again`);
			const secondNameRequest = await saveAndWaitForPut(page, environmentPath);
			expect(secondNameRequest.postDataJSON()).not.toHaveProperty('accessToken');
		});

		test('should update and save the base server URL in Docker settings', async ({ page }) => {
			await openLocalEnvironment(page);
			await expect(page.getByLabel('Agent access token', { exact: true })).toBeHidden();
			await page.getByRole('tab', { name: 'Docker Settings', exact: true }).click();

			const baseServerUrlInput = page.locator('#base-server-url');
			await expect(baseServerUrlInput).toBeVisible();

			const originalBaseServerUrl = await baseServerUrlInput.inputValue();
			const updatedBaseServerUrl = originalBaseServerUrl.endsWith('/e2e')
				? `${originalBaseServerUrl}-2`
				: `${originalBaseServerUrl}/e2e`;

			try {
				await baseServerUrlInput.fill(updatedBaseServerUrl);
				await expect(baseServerUrlInput).toHaveValue(updatedBaseServerUrl);
				const settingsRequest = await saveAndWaitForPut(
					page,
					`/api/environments/${LOCAL_ENV_ID}/settings`
				);
				expect(settingsRequest.postDataJSON()).not.toHaveProperty('accessToken');

				await page.reload();
				await page.getByRole('tab', { name: 'Docker Settings', exact: true }).click();
				await expect(page.locator('#base-server-url')).toHaveValue(updatedBaseServerUrl, {
					timeout: 15000
				});
			} finally {
				if (!page.isClosed()) {
					await page.getByRole('tab', { name: 'Docker Settings', exact: true }).click();
					const currentValue = await page.locator('#base-server-url').inputValue();
					if (currentValue !== originalBaseServerUrl) {
						await page.locator('#base-server-url').fill(originalBaseServerUrl);
						await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
					}
				}
			}
		});

		test('should update and save the follow project symlinks setting from Storage & Limits', async ({
			page
		}) => {
			await openLocalEnvironment(page);
			await page.getByRole('tab', { name: 'Storage & Limits', exact: true }).click();

			const followProjectSymlinksSwitch = page.locator('#follow-project-symlinks');
			await expect(followProjectSymlinksSwitch).toBeVisible();

			const originalChecked =
				(await followProjectSymlinksSwitch.getAttribute('aria-checked')) === 'true';
			const updatedChecked = !originalChecked;

			try {
				await followProjectSymlinksSwitch.click();
				await expect(followProjectSymlinksSwitch).toHaveAttribute(
					'aria-checked',
					String(updatedChecked)
				);

				const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
				await expect(saveButton).toBeEnabled();

				const responsePromise = page.waitForResponse((response) => {
					const request = response.request();
					if (request.method() !== 'PUT') return false;
					const url = new URL(response.url());
					return url.pathname === `/api/environments/${LOCAL_ENV_ID}/settings`;
				});

				await saveButton.click();
				const response = await responsePromise;
				expect(response.ok()).toBeTruthy();

				await page.reload();
				await page.getByRole('tab', { name: 'Storage & Limits', exact: true }).click();
				await expect(page.locator('#follow-project-symlinks')).toHaveAttribute(
					'aria-checked',
					String(updatedChecked)
				);
			} finally {
				if (!page.isClosed()) {
					await page.getByRole('tab', { name: 'Storage & Limits', exact: true }).click();
					const currentChecked =
						(await page.locator('#follow-project-symlinks').getAttribute('aria-checked')) ===
						'true';
					if (currentChecked !== originalChecked) {
						await page.locator('#follow-project-symlinks').click();
						await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
					}
				}
			}
		});

		test('should reset unsaved environment detail changes', async ({ page, registerCleanup }) => {
			const envName = `settings-reset-${Date.now().toString().slice(-5)}`;
			const environmentIds = new Set<string>();
			registerCleanup(() => removeCreatedEnvironments(page, environmentIds, envName));
			const { id: environmentId, apiKey } = await createDirectEnvironmentViaUI(
				page,
				envName,
				environmentIds
			);
			await openEnvironment(page, environmentId);
			const titleButton = environmentTitleButton(page);
			const apiUrlButton = page.getByTitle('API URL', { exact: true });
			const originalApiUrl = (await apiUrlButton.textContent())!.trim();
			await renameEnvironmentInHeader(page, `${envName}-pending`);
			await apiUrlButton.click();
			await page.locator('#api-url').fill(originalApiUrl.replace(':3552', ':3553'));
			await page.locator('#api-url').press('Tab');
			const tokenInput = page.getByLabel('Agent access token', { exact: true });
			await tokenInput.fill(apiKey);

			const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
			const resetButton = page.getByRole('button', { name: 'Reset', exact: true }).first();
			await expect(saveButton).toBeEnabled();
			await expect(resetButton).toBeVisible();
			await resetButton.click();
			await expect(titleButton).toHaveText(envName);
			await expect(apiUrlButton).toHaveText(originalApiUrl);
			await expect(tokenInput).toBeHidden();
			await expect(saveButton).toBeDisabled();

			await apiUrlButton.click();
			await page.locator('#api-url').fill(originalApiUrl.replace(':3552', ':3553'));
			await page.locator('#api-url').press('Tab');
			await expect(tokenInput).toHaveValue('');
			await tokenInput.fill(apiKey);
			await page.getByRole('link', { name: 'Back', exact: true }).first().click();
			await expect(page).toHaveURL(/\/environments$/);
			await expect(page.getByRole('heading', { name: 'Environments', exact: true })).toBeVisible();
			await page.getByRole('button', { name: envName, exact: true }).click();
			await expect(page).toHaveURL(new RegExp(`/environments/${environmentId}(?:\\?|$)`));
			await expect(environmentTitleButton(page)).toHaveText(envName);
			await apiUrlButton.click();
			await page.locator('#api-url').fill(originalApiUrl.replace(':3552', ':3553'));
			await page.locator('#api-url').press('Tab');
			await expect(tokenInput).toHaveValue('');
		});

		test('should update and save the default deploy pull policy in Docker settings', async ({
			page
		}) => {
			await openLocalEnvironment(page);

			const dockerTab = page.getByRole('tab', { name: 'Docker Settings', exact: true });
			await dockerTab.click();
			const pullPolicyTrigger = page.locator('#defaultDeployPullPolicy');
			await expect(pullPolicyTrigger).toBeVisible();

			const originalValue = (await pullPolicyTrigger.textContent())?.trim() || 'Missing';
			const updatedValue = originalValue.includes('Always') ? 'Never' : 'Always';

			try {
				await selectSettingOption(page, pullPolicyTrigger, updatedValue);
				await expect(pullPolicyTrigger).toContainText(updatedValue);
				await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);

				await page.reload();
				await page.getByRole('tab', { name: 'Docker Settings', exact: true }).click();
				await expect(page.locator('#defaultDeployPullPolicy')).toContainText(updatedValue, {
					timeout: 15000
				});
			} finally {
				if (!page.isClosed()) {
					await page.getByRole('tab', { name: 'Docker Settings', exact: true }).click();
					const currentValue = (
						(await page.locator('#defaultDeployPullPolicy').textContent()) || ''
					).trim();
					if (!currentValue.includes(originalValue)) {
						await selectSettingOption(
							page,
							page.locator('#defaultDeployPullPolicy'),
							originalValue
						);
						await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
					}
				}
			}
		});

		test('should save decimal trivy CPU limits and reject negative values', async ({ page }) => {
			test.setTimeout(120_000);
			const settingsPath = `/api/environments/${LOCAL_ENV_ID}/settings`;
			const settingsResponse = await page.request.get(settingsPath);
			expect(settingsResponse.ok()).toBeTruthy();
			const settings = (await settingsResponse.json()) as Array<{ key: string; value: string }>;
			const originalSettings = Object.fromEntries(
				settings
					.filter(({ key }) =>
						['trivyResourceLimitsEnabled', 'trivyCpuLimit', 'trivyMemoryLimitMb'].includes(key)
					)
					.map(({ key, value }) => [key, value])
			);
			expect(Object.keys(originalSettings)).toHaveLength(3);

			try {
				await openLocalEnvironment(page);
				await page.getByRole('tab', { name: 'Security', exact: true }).click();
				await page.locator('#trivyResourceLimitsEnabledSwitch').setChecked(true);
				const cpuInput = page.getByRole('spinbutton', { name: 'CPU Limit (cores)', exact: true });
				await expect(cpuInput).toHaveAttribute('min', '0');
				await expect(cpuInput).toHaveAttribute('step', 'any');

				const values = ['0.5', '1.5', '2.5', '0.25', '1', '0'];
				if ((await cpuInput.inputValue()) === values[0]) values.reverse();

				for (const value of values) {
					await cpuInput.fill(value);
					expect(await cpuInput.evaluate((input: HTMLInputElement) => input.validity.valid)).toBe(
						true
					);
					const requestPromise = page.waitForRequest(
						(request) =>
							request.method() === 'PUT' && new URL(request.url()).pathname === settingsPath
					);
					await saveAndWaitForPut(page, settingsPath);
					const payload = (await requestPromise).postDataJSON() as Record<string, unknown>;
					expect(payload.trivyCpuLimit).toBe(value);
					expect(payload.trivyResourceLimitsEnabled).toBe('true');
					await page.reload();
					await page.getByRole('tab', { name: 'Security', exact: true }).click();
					await expect(cpuInput).toHaveValue(value);
				}

				const writes: Request[] = [];
				const recordWrite = (request: Request) => {
					if (request.method() === 'PUT' && new URL(request.url()).pathname === settingsPath) {
						writes.push(request);
					}
				};
				page.on('request', recordWrite);
				try {
					await cpuInput.fill('-0.5');
					await page.getByRole('button', { name: 'Save', exact: true }).first().click();
					await expect(
						page.getByText('Please check the form for errors.', { exact: true })
					).toBeVisible();
					expect(writes).toHaveLength(0);
					await page.reload();
					await page.getByRole('tab', { name: 'Security', exact: true }).click();
					await expect(cpuInput).toHaveValue(values[values.length - 1]);
				} finally {
					page.off('request', recordWrite);
				}
			} finally {
				const restored = await page.request.put(settingsPath, { data: originalSettings });
				expect(restored.ok()).toBeTruthy();
			}
		});

		test('should update and save the trivy network mode including auto', async ({ page }) => {
			await openLocalEnvironment(page);
			await page.getByRole('tab', { name: 'Security', exact: true }).click();

			const trivyNetworkTrigger = page.locator('#trivyNetwork');
			await expect(trivyNetworkTrigger).toBeVisible();

			const originalValue = ((await trivyNetworkTrigger.textContent()) || '').trim();
			const updatedValue = originalValue.includes('bridge') ? 'Auto' : 'bridge';

			try {
				await selectSettingOption(page, trivyNetworkTrigger, updatedValue);
				await expect(trivyNetworkTrigger).toContainText(updatedValue);

				const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
				await expect(saveButton).toBeEnabled();

				const responsePromise = page.waitForResponse((response) => {
					const request = response.request();
					if (request.method() !== 'PUT') return false;
					const url = new URL(response.url());
					return url.pathname === `/api/environments/${LOCAL_ENV_ID}/settings`;
				});

				await saveButton.click();
				const response = await responsePromise;
				expect(response.ok()).toBeTruthy();

				await page.reload();
				await page.getByRole('tab', { name: 'Security', exact: true }).click();
				await expect(page.locator('#trivyNetwork')).toContainText(updatedValue);
			} finally {
				if (!page.isClosed()) {
					await page.getByRole('tab', { name: 'Security', exact: true }).click();
					const currentValue = ((await page.locator('#trivyNetwork').textContent()) || '').trim();
					if (!currentValue.includes(originalValue)) {
						await selectSettingOption(page, page.locator('#trivyNetwork'), originalValue);
						await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
					}
				}
			}
		});
	});
});

test.describe('Switching', () => {
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
		await page.context().route(/\/api\/stream(?:\?.*)?$/, async (route) => {
			const channels =
				new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
			const timestamp = new Date().toISOString();
			await route.fulfill({
				status: 200,
				contentType: 'application/x-json-stream',
				body: channels.includes('environments')
					? `${JSON.stringify({
							channel: 'environments',
							environment: {
								type: 'snapshot',
								environments: [localEnvironment, remoteEnvironment],
								timestamp
							},
							timestamp
						})}\n`
					: ''
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
							body: JSON.stringify(
								paginated([containerSummary('remote-b-id', 'remote-b-container')])
							)
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
			await expect(
				page.getByRole('link', { name: 'local-a-container', exact: true })
			).toBeVisible();

			try {
				await selectRemoteEnvironment(page);
				await remoteStarted;
				await expect(
					page.getByRole('link', { name: 'local-a-container', exact: true })
				).toHaveCount(0);
				await page.waitForTimeout(200);
				expect(websocketPaths).not.toContain(
					'/api/environments/remote-switch-test/ws/containers/local-a-id/stats'
				);
			} finally {
				releaseRemote();
			}

			await expect(
				page.getByRole('link', { name: 'remote-b-container', exact: true })
			).toBeVisible();
			await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toHaveCount(
				0
			);
		});

		test('does not restore local rows when the remote request fails', async ({
			page,
			pageErrorGuard
		}) => {
			pageErrorGuard.allow('remote unavailable');
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
			await expect(
				page.getByRole('link', { name: 'local-a-container', exact: true })
			).toBeVisible();
			const failedRemoteRequest = page.waitForResponse((response) => {
				return (
					new URL(response.url()).pathname === '/api/environments/remote-switch-test/containers' &&
					response.status() === 503
				);
			});
			await selectRemoteEnvironment(page);
			await failedRemoteRequest;
			await expect(page.getByRole('link', { name: 'local-a-container', exact: true })).toHaveCount(
				0
			);
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
				const id = decodeURIComponent(
					new URL(route.request().url()).pathname.split('/').pop() ?? ''
				);
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
			await expect(page.getByRole('tab', { name: 'Logs' })).toHaveAttribute(
				'aria-selected',
				'true'
			);

			await page.evaluate(() => {
				const link = document.createElement('a');
				link.id = 'same-route-navigation';
				link.href = '/containers/route-b';
				link.textContent = 'navigate';
				document.body.append(link);
			});
			await page.locator('#same-route-navigation').click();
			await expect(page).toHaveURL('/containers/route-b?tab=overview');
			await expect(page.getByRole('heading', { name: 'Route B Container' })).toBeVisible();
			await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute(
				'aria-selected',
				'true'
			);

			await page.getByRole('button', { name: 'More actions', exact: true }).click();
			await page.getByRole('menuitem', { name: 'Redeploy', exact: true }).click();
			const dialog = page.getByRole('dialog');
			await dialog.getByRole('button', { name: 'Redeploy', exact: true }).click();
			await expect(page).toHaveURL('/containers/route-redeployed?tab=overview');
			await expect(page.getByRole('heading', { name: 'Redeployed Container' })).toBeVisible();
		});
	});
});

test.describe('Edge agents', () => {
	const ROUTES = {
		environments: '/environments'
	};

	async function openNewEnvironmentSheet(page: Page) {
		await page.goto(ROUTES.environments);
		await page.waitForLoadState('load');

		const addButton = page.getByRole('button', { name: 'Add Environment', exact: true });
		await expect(addButton).toBeVisible();
		await addButton.click();

		await expect(page.getByText('Create New Agent Environment')).toBeVisible();
	}

	async function switchToEdgeMode(page: Page) {
		await page.getByRole('tab', { name: 'Edge', exact: true }).click();
		await expect(page.getByText('Agent connects outbound to the manager.')).toBeVisible();
	}

	test.describe('Edge Agent Environment', () => {
		test('should switch between direct and edge connection modes', async ({ page }) => {
			await openNewEnvironmentSheet(page);

			await expect(page.locator('#new-agent-api-url')).toBeVisible();
			await expect(page.getByPlaceholder('Remote Docker Host')).toBeHidden();

			await switchToEdgeMode(page);
			await expect(page.locator('#new-agent-api-url')).toBeHidden();
			await expect(page.getByPlaceholder('Remote Docker Host')).toBeVisible();

			await page.getByRole('tab', { name: 'Direct', exact: true }).click();
			await expect(page.locator('#new-agent-api-url')).toBeVisible();
			await expect(page.getByPlaceholder('Remote Docker Host')).toBeHidden();
		});

		test('should validate required fields for edge and direct modes', async ({ page }) => {
			await openNewEnvironmentSheet(page);
			let createRequests = 0;

			await page.route('**/api/environments', async (route) => {
				if (route.request().method() === 'POST') {
					createRequests += 1;
				}
				await route.continue();
			});

			// Direct mode: missing name and URL
			await page.getByRole('button', { name: 'Generate Agent Configuration', exact: true }).click();
			await expect.poll(() => createRequests).toBe(0);

			// Edge mode: missing name
			await switchToEdgeMode(page);
			await page.getByRole('button', { name: 'Generate Agent Configuration', exact: true }).click();
			await expect.poll(() => createRequests).toBe(0);
		});

		test('should create an edge agent environment and show deployment snippets', async ({
			page,
			registerCleanup
		}) => {
			const environmentName = `edge-agent-${Date.now().toString().slice(-6)}`;
			let createdEnvironmentId: string | null = null;
			const environmentIds = new Set<string>();
			registerCleanup(() => removeCreatedEnvironments(page, environmentIds, environmentName));
			const agentSettingsRequests: string[] = [];
			page.on('request', (request) => {
				if (!createdEnvironmentId) return;
				const pathname = new URL(request.url()).pathname;
				if (
					pathname === `/api/environments/${createdEnvironmentId}/settings` ||
					pathname === `/api/environments/${createdEnvironmentId}/settings/public`
				) {
					agentSettingsRequests.push(pathname);
				}
			});

			await page.route('**/api/environments', async (route) => {
				if (route.request().method() === 'POST') {
					const response = await route.fetch();
					const environment = await readApiData<{ id: string }>(
						response,
						'Create edge environment'
					);
					createdEnvironmentId = environment.id;
					environmentIds.add(environment.id);
					await route.fulfill({ response });
					return;
				}

				await route.continue();
			});

			await openNewEnvironmentSheet(page);
			await switchToEdgeMode(page);

			await page.getByPlaceholder('Remote Docker Host').fill(environmentName);
			const submitButton = page.getByRole('button', {
				name: 'Generate Agent Configuration',
				exact: true
			});
			await submitButton.click();

			const sheetTitle = page.locator('[data-slot="sheet-title"]');
			await expect(sheetTitle).toContainText('Environment Created');
			await expect(
				page.getByText('Edge agent - connects outbound to manager', { exact: true })
			).toBeVisible();
			await expect(page.getByText('API Key', { exact: true })).toBeVisible();
			await expect(page.getByText('Docker Run Command', { exact: true })).toBeVisible();
			await expect(page.getByText('Docker Compose', { exact: true })).toBeVisible();

			const snippetBlocks = page.locator('pre code').filter({ hasText: 'EDGE_AGENT=true' });
			await expect(snippetBlocks.first()).toBeVisible();

			const dockerRunSnippet = snippetBlocks.first();
			await expect(dockerRunSnippet).toContainText('arcane-edge-agent');
			await expect(dockerRunSnippet).not.toContainText('-p 3553:3553');

			await page.getByRole('button', { name: 'Done', exact: true }).click();

			await expect(page.getByRole('button', { name: environmentName, exact: true })).toBeVisible();
			const environmentRow = page.locator('tr').filter({
				has: page.getByRole('button', { name: environmentName, exact: true })
			});
			await expect(environmentRow.getByText('edge://edge-agent-').first()).toBeVisible();
			await expect(environmentRow.getByText('Edge', { exact: true })).toBeVisible();

			await page.getByRole('button', { name: environmentName, exact: true }).click();
			if (createdEnvironmentId) {
				await expect(page).toHaveURL(new RegExp(`/environments/${createdEnvironmentId}`));
			}

			await expect(page.getByTitle('API URL', { exact: true })).toHaveText(/edge:\/\/edge-agent-/);
			await expect(page.getByText('Edge', { exact: true }).first()).toBeVisible();
			await expect(page.getByText('Live Tunnel', { exact: true })).toBeVisible();
			await expect(
				page.getByRole('tab', { name: 'Connection & Edge', exact: true })
			).toHaveAttribute('data-state', 'active');
			expect(agentSettingsRequests).toEqual([]);
		});
	});
});
