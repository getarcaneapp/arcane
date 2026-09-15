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

async function createDirectEnvironmentViaUI(page: Page, environmentName: string) {
	await page.goto('/environments');
	await page.waitForLoadState('load');

	await page.getByRole('button', { name: 'Add Environment', exact: true }).click();
	await expect(page.getByText('Create New Agent Environment')).toBeVisible();

	const dialog = page.getByRole('dialog');
	await dialog.getByLabel('Name', { exact: true }).fill(environmentName);
	await dialog.getByLabel('Agent Address', { exact: true }).fill('localhost:3552');
	const createResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' &&
			new URL(response.url()).pathname === '/api/environments'
	);
	await page.getByRole('button', { name: 'Generate Agent Configuration', exact: true }).click();
	const createResponse = await createResponsePromise;
	expect(createResponse.ok(), await createResponse.text()).toBeTruthy();
	const created: { data: { id: string } } = await createResponse.json();
	expect(created.data.id).toBeTruthy();

	await expect(
		page.getByRole('heading', { name: 'Environment Created Successfully', exact: true })
	).toBeVisible();
	await page.getByRole('button', { name: 'Done', exact: true }).click();
	await expect(page.getByRole('button', { name: environmentName, exact: true })).toBeVisible();
	return created.data.id;
}

async function deleteEnvironmentViaUI(page: Page, environmentName: string) {
	await page.goto('/environments');
	await page.waitForLoadState('load');

	const envRow = page.getByRole('row').filter({
		has: page.getByRole('button', { name: environmentName, exact: true })
	});

	if ((await envRow.count()) === 0) {
		return;
	}

	await envRow.hover();
	await envRow.getByRole('button', { name: 'Open menu', exact: true }).click();
	await page.getByRole('menuitem', { name: 'Delete', exact: true }).click();
	await page.getByRole('button', { name: 'Remove', exact: true }).click();
	await expect(page.getByRole('button', { name: environmentName, exact: true })).toHaveCount(0);
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
		let environmentId = '';

		try {
			environmentId = await createDirectEnvironmentViaUI(page, envName);
			await page.getByRole('button', { name: envName, exact: true }).click();
			await expect(page).toHaveURL(/\/environments\/[^/?]+\?tab=[a-z]+$/);

			await renameEnvironmentInHeader(page, updatedName);
			await saveAndWaitForPut(page, `/api/environments/${environmentId}`);

			await page.reload();
			await expect(environmentTitleButton(page)).toHaveText(updatedName);
		} finally {
			if (environmentId) {
				await page.request.delete(`/api/environments/${environmentId}`).catch(() => undefined);
			}
			await deleteEnvironmentViaUI(page, updatedName);
			await deleteEnvironmentViaUI(page, envName);
		}
	});

	test('should update and save the base server URL in Docker settings', async ({ page }) => {
		await openLocalEnvironment(page);
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
			await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);

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
		await openLocalEnvironment(page);

		const titleButton = environmentTitleButton(page);
		const originalName = (await titleButton.textContent())!.trim();
		await renameEnvironmentInHeader(page, `${originalName}-pending`);

		const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
		const resetButton = page.getByRole('button', { name: 'Reset', exact: true }).first();

		await expect(saveButton).toBeEnabled();
		await expect(resetButton).toBeVisible();
		await resetButton.click();

		await expect(titleButton).toHaveText(originalName);
		await expect(saveButton).toBeDisabled();
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
