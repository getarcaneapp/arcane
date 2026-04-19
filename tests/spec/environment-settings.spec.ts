import { test, expect, type Locator, type Page } from '@playwright/test';

const LOCAL_ENV_ID = '0';

async function openEnvironment(page: Page, environmentId: string) {
	await page.goto(`/environments/${environmentId}`);
	await page.waitForLoadState('networkidle');
	await expect(page.locator('#env-name')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Save', exact: true }).first()).toBeVisible();
}

async function createDirectEnvironmentViaUI(page: Page, environmentName: string) {
	await page.goto('/environments');
	await page.waitForLoadState('networkidle');

	await page.getByRole('button', { name: 'Add Environment', exact: true }).click();
	await expect(page.getByText('Create New Agent Environment')).toBeVisible();

	await page.locator('input#name:visible').first().fill(environmentName);
	await page.locator('#new-agent-api-url').fill('localhost:3552');
	await page.getByRole('button', { name: 'Generate Agent Configuration', exact: true }).click();

	await expect(page.locator('[data-slot="sheet-title"]')).toContainText(/Environment Created/i);
	await page.getByRole('button', { name: 'Done', exact: true }).click();
	await expect(page.getByRole('button', { name: environmentName, exact: true })).toBeVisible();
}

async function deleteEnvironmentViaUI(page: Page, environmentName: string) {
	await page.goto('/environments');
	await page.waitForLoadState('networkidle');

	const envRow = page.locator('tr').filter({
		has: page.getByRole('button', { name: environmentName, exact: true })
	});

	if ((await envRow.count()) === 0) {
		return;
	}

	await envRow.getByRole('button', { name: /open menu/i }).click();
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
	const option = page
		.locator('[data-slot="select-item"], [data-slot="command-item"]')
		.filter({ hasText: optionText })
		.first();
	await expect(option).toBeVisible();
	await option.click();
}

test.describe('Environment Settings UI', () => {
	test.describe.configure({ mode: 'serial' });

	test('should update and save environment details', async ({ page }) => {
		test.setTimeout(120_000); // 120 seconds timeout for this lengthy UI workflow
		const envName = `settings-ui-${Date.now().toString().slice(-5)}`;
		const updatedName = `${envName}-updated`;

		try {
			await createDirectEnvironmentViaUI(page, envName);
			await page.getByRole('button', { name: envName, exact: true }).click();
			await expect(page).toHaveURL(/\/environments\/[^/]+$/);

			const environmentId = new URL(page.url()).pathname.split('/').pop()!;
			const nameInput = page.locator('#env-name');
			await nameInput.fill(updatedName);
			await saveAndWaitForPut(page, `/api/environments/${environmentId}`);

			await page.reload();
			await expect(page.locator('#env-name')).toHaveValue(updatedName);
		} finally {
			await deleteEnvironmentViaUI(page, updatedName);
			await deleteEnvironmentViaUI(page, envName);
		}
	});

	test('should update and save general environment settings', async ({ page }) => {
		await openLocalEnvironment(page);
		await page.getByRole('tab', { name: 'General', exact: true }).click();

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
			await page.getByRole('tab', { name: 'General', exact: true }).click();
			await expect(page.locator('#base-server-url')).toHaveValue(updatedBaseServerUrl, {
				timeout: 15000
			});
		} finally {
			if (!page.isClosed()) {
				await page.getByRole('tab', { name: 'General', exact: true }).click();
				const currentValue = await page.locator('#base-server-url').inputValue();
				if (currentValue !== originalBaseServerUrl) {
					await page.locator('#base-server-url').fill(originalBaseServerUrl);
					await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
				}
			}
		}
	});

	test('should update and save the follow project symlinks setting from General', async ({
		page
	}) => {
		await openLocalEnvironment(page);
		await page.getByRole('tab', { name: 'General', exact: true }).click();

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
			await page.getByRole('tab', { name: 'General', exact: true }).click();
			await expect(page.locator('#follow-project-symlinks')).toHaveAttribute(
				'aria-checked',
				String(updatedChecked)
			);
		} finally {
			if (!page.isClosed()) {
				await page.getByRole('tab', { name: 'General', exact: true }).click();
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

		const nameInput = page.locator('#env-name');
		const originalName = await nameInput.inputValue();
		await nameInput.fill(`${originalName}-pending`);

		const saveButton = page.getByRole('button', { name: 'Save', exact: true }).first();
		const resetButton = page.getByRole('button', { name: 'Reset', exact: true }).first();

		await expect(saveButton).toBeEnabled();
		await expect(resetButton).toBeVisible();
		await resetButton.click();

		await expect(nameInput).toHaveValue(originalName);
		await expect(saveButton).toBeDisabled();
	});

	test('should update and save the default deploy pull policy in Docker settings', async ({
		page
	}) => {
		await openLocalEnvironment(page);

		const dockerTab = page.getByRole('tab', { name: /Docker/i }).first();
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
			await page
				.getByRole('tab', { name: /Docker/i })
				.first()
				.click();
			await expect(page.locator('#defaultDeployPullPolicy')).toContainText(updatedValue, {
				timeout: 15000
			});
		} finally {
			if (!page.isClosed()) {
				await page
					.getByRole('tab', { name: /Docker/i })
					.first()
					.click();
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

	test('should update and save the trivy cache prune preservation setting', async ({ page }) => {
		await openLocalEnvironment(page);
		await page.getByRole('tab', { name: 'Security', exact: true }).click();

		const preserveCacheSwitch = page.locator('#trivyPreserveCacheOnVolumePruneSwitch');
		await expect(preserveCacheSwitch).toBeVisible();

		const originalChecked = (await preserveCacheSwitch.getAttribute('aria-checked')) === 'true';
		const updatedChecked = !originalChecked;

		try {
			await preserveCacheSwitch.click();
			await expect(preserveCacheSwitch).toHaveAttribute('aria-checked', String(updatedChecked));

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
			await expect(page.locator('#trivyPreserveCacheOnVolumePruneSwitch')).toHaveAttribute(
				'aria-checked',
				String(updatedChecked)
			);
		} finally {
			if (!page.isClosed()) {
				await page.getByRole('tab', { name: 'Security', exact: true }).click();
				const currentChecked =
					(await page
						.locator('#trivyPreserveCacheOnVolumePruneSwitch')
						.getAttribute('aria-checked')) === 'true';
				if (currentChecked !== originalChecked) {
					await page.locator('#trivyPreserveCacheOnVolumePruneSwitch').click();
					await saveAndWaitForPut(page, `/api/environments/${LOCAL_ENV_ID}/settings`);
				}
			}
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
