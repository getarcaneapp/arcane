import { test, expect, type Page } from '../fixtures/test.fixture';

test.describe('Notification settings', () => {
	const openProviderTab = async (page: Page, name: string) => {
		const tab = page.getByRole('tab', { name });
		await tab.scrollIntoViewIfNeeded();
		await expect(tab).toBeVisible();
		await tab.click();
		await expect(page.getByRole('tabpanel').filter({ visible: true })).toBeVisible();
	};

	const enableCurrentProvider = async (page: Page) => {
		const providerPanel = page.getByRole('tabpanel').filter({ visible: true });
		const toggle = providerPanel.getByRole('switch').first();
		await expect(toggle).toBeVisible();
		await toggle.click();
		await expect(providerPanel.locator('[data-dropdown-menu-trigger]')).toBeVisible({
			timeout: 10000
		});
	};

	const openTestMenu = async (page: Page) => {
		const trigger = page
			.getByRole('tabpanel')
			.filter({ visible: true })
			.locator('[data-dropdown-menu-trigger]');
		await expect(trigger).toBeVisible({ timeout: 10000 });
		await trigger.click();
	};

	// Shared setup for all notification tests
	const setupNotificationTest = async (
		page: Page,
		provider: string,
		options: { failProviders?: ReadonlySet<string> } = {}
	) => {
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
		const attemptedProviders: string[] = [];
		const savedProviders: string[] = [];
		// Saved settings are kept in memory and served back on GET so that a
		// reload round-trips them through the form like the real backend would.
		const persistedSettings: Array<Record<string, unknown>> = [];

		await page.route('**/api/environments/*/notifications/settings', async (route) => {
			const req = route.request();
			if (req.method() === 'GET') {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(persistedSettings)
				});
				return;
			}

			if (req.method() === 'POST') {
				saveEndpointCalled = true;
				const saved = req.postDataJSON() as Record<string, unknown>;
				const savedProvider = String(saved.provider ?? '');
				attemptedProviders.push(savedProvider);
				if (options.failProviders?.has(savedProvider)) {
					await route.fulfill({
						status: 500,
						contentType: 'application/json',
						body: JSON.stringify({ error: `${savedProvider} save failed` })
					});
					return;
				}
				savedProviders.push(savedProvider);
				const index = persistedSettings.findIndex((s) => s.provider === saved.provider);
				if (index >= 0) {
					persistedSettings[index] = saved;
				} else {
					persistedSettings.push(saved);
				}
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(saved)
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
		await page.waitForLoadState('load');
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
			wasSaveEndpointCalled: () => saveEndpointCalled,
			getAttemptedProviders: () => [...attemptedProviders],
			getSavedProviders: () => [...savedProviders]
		};
	};

	test('saves only changed providers and retains failed providers as dirty', async ({ page }) => {
		const { getAttemptedProviders, getSavedProviders } = await setupNotificationTest(
			page,
			'discord',
			{
				failProviders: new Set(['email'])
			}
		);

		await openProviderTab(page, 'Discord');
		await enableCurrentProvider(page);
		await page.getByPlaceholder('Enter webhook ID').fill('123456789');
		await page.getByPlaceholder('Enter webhook token').fill('abc-def-ghi');

		await openProviderTab(page, 'Email');
		await enableCurrentProvider(page);
		await page.getByPlaceholder('smtp.example.com').fill('smtp.example.com');
		await page.getByPlaceholder('notifications@example.com').fill('notifications@example.com');
		await page.getByPlaceholder('user1@example.com, user2@example.com').fill('user1@example.com');

		const saveButton = page.getByRole('button', { name: 'Save', exact: true });
		await saveButton.click();
		await expect.poll(getAttemptedProviders).toEqual(['email', 'discord']);
		await expect.poll(getSavedProviders).toEqual(['discord']);
		await expect(page.getByText(/Failed to save Email settings:/)).toBeVisible();
		await expect(saveButton).toBeEnabled();

		await saveButton.click();
		await expect.poll(getAttemptedProviders).toEqual(['email', 'discord', 'email']);
	});

	test('should persist the selected provider tab in the URL', async ({ page }) => {
		await setupNotificationTest(page, 'discord');
		await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('email');

		await openProviderTab(page, 'Discord');
		await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('discord');

		await page.reload();
		await expect(page.getByRole('tab', { name: 'Discord' })).toHaveAttribute(
			'data-state',
			'active'
		);
		await expect(page.getByRole('tabpanel', { name: 'Discord', exact: true })).toBeVisible();
	});

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

	test('should allow configuring a custom generic webhook payload template', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'generic');

		await openProviderTab(page, 'Generic');
		await enableCurrentProvider(page);

		await page.getByPlaceholder('https://example.com/webhook').fill('https://example.com/webhook');

		const template = '{"receiveIdType":"chat_id","msgType":"text","text":"{{.message}}"}';
		await page.locator('#generic-payload-template').fill(template);

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();

		// The template must survive a reload, proving it round-trips through the
		// save path and back into the form.
		await page.reload();
		await openProviderTab(page, 'Generic');
		await expect(page.locator('#generic-payload-template')).toHaveValue(template);
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
