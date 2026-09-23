import { waitForDialogReady } from '../utils/playwright.util';
import { removeApiResource } from '../utils/fetch.util';
import { test, expect, type Locator, type Page, type Request } from '../fixtures/test.fixture';

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
			const currentResponse = await page.request.get(`/api/environments/${LOCAL_ENV_ID}/settings`);
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
			const restoredResponse = await page.request.get(`/api/environments/${LOCAL_ENV_ID}/settings`);
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

		const jobScheduleCategory = page.getByRole('button').filter({ hasText: 'Automations' }).first();
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

	test('should update and save environment details', async ({ page }) => {
		test.setTimeout(120_000); // 120 seconds timeout for this lengthy UI workflow
		const envName = `settings-ui-${Date.now().toString().slice(-5)}`;
		const updatedName = `${envName}-updated`;
		const environmentIds = new Set<string>();

		try {
			const { id: environmentId, apiKey } = await createDirectEnvironmentViaUI(
				page,
				envName,
				environmentIds
			);
			const environmentPath = `/api/environments/${environmentId}`;
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
		} finally {
			for (const id of environmentIds) await removeApiResource(page, `/api/environments/${id}`);
		}
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
					(await page.locator('#follow-project-symlinks').getAttribute('aria-checked')) === 'true';
				if (currentChecked !== originalChecked) {
					await page.locator('#follow-project-symlinks').click();
					await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
				}
			}
		}
	});

	test('should reset unsaved environment detail changes', async ({ page }) => {
		const envName = `settings-reset-${Date.now().toString().slice(-5)}`;
		const environmentIds = new Set<string>();
		try {
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
			await page.getByRole('button', { name: 'Back to Environments', exact: true }).click();
			await expect(page).toHaveURL(/\/environments$/);
			await expect(page.getByRole('heading', { name: 'Environments', exact: true })).toBeVisible();
			await page.getByRole('button', { name: envName, exact: true }).click();
			await expect(page).toHaveURL(new RegExp(`/environments/${environmentId}(?:\\?|$)`));
			await expect(environmentTitleButton(page)).toHaveText(envName);
			await apiUrlButton.click();
			await page.locator('#api-url').fill(originalApiUrl.replace(':3552', ':3553'));
			await page.locator('#api-url').press('Tab');
			await expect(tokenInput).toHaveValue('');
		} finally {
			for (const id of environmentIds) await removeApiResource(page, `/api/environments/${id}`);
		}
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
					await selectSettingOption(page, page.locator('#defaultDeployPullPolicy'), originalValue);
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
