import { test, expect, type Page } from '@playwright/test';

test.describe('Notification settings', () => {
	const openProviderTab = async (page: Page, name: string) => {
		const tab = page.getByRole('tab', { name });
		await tab.scrollIntoViewIfNeeded();
		await expect(tab).toBeVisible();
		await tab.click();
		await expect(page.locator('h3').filter({ hasText: name }).first()).toBeVisible();
	};

	const enableCurrentProvider = async (page: Page) => {
		const toggle = page.getByRole('switch').first();
		await expect(toggle).toBeVisible();
		await toggle.click();
		await expect(page.locator('[data-dropdown-menu-trigger]').last()).toBeVisible({
			timeout: 10000
		});
	};

	const openTestMenu = async (page: Page) => {
		const trigger = page.locator('[data-dropdown-menu-trigger]').last();
		await expect(trigger).toBeVisible({ timeout: 10000 });
		await trigger.click();
	};

	// Shared setup for all notification tests
	const setupNotificationTest = async (page: Page, provider: string) => {
		const observedErrors: string[] = [];

		page.on('pageerror', (err) => {
			observedErrors.push(String(err?.message ?? err));
		});

		page.on('console', (msg) => {
			if (msg.type() === 'error') {
				observedErrors.push(msg.text());
			}
		});

		let saveEndpointCalled = false;
		let testEndpointCalled = false;

		await page.route('**/api/environments/*/notifications/settings', async (route) => {
			const req = route.request();
			if (req.method() === 'GET') {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
				return;
			}

			if (req.method() === 'POST') {
				saveEndpointCalled = true;
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ success: true })
				});
				return;
			}

			await route.continue();
		});

		await page.route('**/api/environments/*/notifications/apprise', async (route) => {
			if (route.request().method() === 'GET') {
				await route.fulfill({
					status: 404,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'not configured' })
				});
				return;
			}

			await route.continue();
		});

		// Stub the specific test endpoint
		await page.route(`**/api/environments/*/notifications/test/${provider}**`, async (route) => {
			testEndpointCalled = true;
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true })
			});
		});

		await page.goto('/settings/notifications');
		await page.waitForLoadState('networkidle');
		await expect(page.getByRole('tab', { name: 'Built-in Notifications' })).toBeVisible();
		await expect(page.getByRole('tab', { name: 'Email' })).toBeVisible();

		return {
			getErrorCheck: () => {
				const stateUnsafe = observedErrors.filter((e) => e.includes('state_unsafe_mutation'));
				expect(
					stateUnsafe,
					`Unexpected state_unsafe_mutation errors: ${stateUnsafe.join('\n')}`
				).toHaveLength(0);
			},
			wasTestEndpointCalled: () => testEndpointCalled,
			wasSaveEndpointCalled: () => saveEndpointCalled
		};
	};

	test('should allow testing email notifications without state_unsafe_mutation errors', async ({
		page
	}) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'email');

		await openProviderTab(page, 'Email');
		await enableCurrentProvider(page);

		// Fill fields
		await page.getByPlaceholder('smtp.example.com').fill('smtp.example.com');
		await page.getByPlaceholder('notifications@example.com').fill('notifications@example.com');
		await page.getByPlaceholder('user1@example.com, user2@example.com').fill('user1@example.com');

		// Trigger test
		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		// Handle Save & Test if needed
		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing discord notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'discord');

		await openProviderTab(page, 'Discord');
		await enableCurrentProvider(page);

		// Discord split fields
		await page.getByPlaceholder('Enter webhook ID').fill('123456789');
		await page.getByPlaceholder('Enter webhook token').fill('abc-def-ghi');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing slack notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'slack');

		await openProviderTab(page, 'Slack');
		await enableCurrentProvider(page);

		// Slack OAuth token (xoxb- or xoxp- format)
		await page
			.getByPlaceholder('xoxb-... or xoxp-...')
			.fill('xoxb-123456789012-1234567890123-abcdefghijklmnopqrstuvwx');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing telegram notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'telegram');

		await openProviderTab(page, 'Telegram');
		await enableCurrentProvider(page);

		// Telegram fields (placeholders are hardcoded in component)
		await page
			.getByPlaceholder('123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11')
			.fill('123456:TEST-TOKEN');
		await page.getByPlaceholder('@channel, 123456789, @another_channel').fill('123456789');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing generic webhook notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'generic');

		await openProviderTab(page, 'Generic');
		await enableCurrentProvider(page);

		await page.getByPlaceholder('https://example.com/webhook').fill('https://example.com/webhook');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing signal notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'signal');

		await openProviderTab(page, 'Signal');
		await enableCurrentProvider(page);

		await page.getByPlaceholder('localhost').fill('signal-api.example.com');
		await page.getByPlaceholder('8080').fill('8080');
		await page.locator('#signal-source').fill('+1234567890');
		await page.locator('#signal-recipients').fill('+1987654321');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('should allow testing ntfy notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'ntfy');

		await openProviderTab(page, 'Ntfy');
		await enableCurrentProvider(page);

		await page.getByPlaceholder('ntfy.sh').fill('ntfy.sh');
		await page.getByPlaceholder('my-updates').fill('arcane-updates');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});
});
