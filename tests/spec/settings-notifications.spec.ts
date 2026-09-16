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
		options: {
			failProviders?: ReadonlySet<string>;
			initialSettings?: Array<Record<string, unknown>>;
		} = {}
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
		const persistedSettings: Array<Record<string, unknown>> = [...(options.initialSettings ?? [])];
		const savedPayloads: Array<Record<string, unknown>> = [];

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
				savedPayloads.push(saved);
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
			getSavedProviders: () => [...savedProviders],
			getSavedPayloads: () => [...savedPayloads]
		};
	};

	const telegramSettings = (chatIds: string[]) => ({
		provider: 'telegram',
		enabled: true,
		config: {
			chatIds,
			preview: true,
			notification: true,
			title: '',
			events: {
				image_update: true,
				container_update: true,
				vulnerability_found: true,
				prune_report: true,
				auto_heal: true
			}
		}
	});

	const telegramChatIds = (payload: Record<string, unknown>) =>
		(payload.config as { chatIds: string[] }).chatIds;

	const telegramRow = (page: Page, index: number) => ({
		chatId: page.locator(`#telegram-chat-id-${index}`),
		topicId: page.locator(`#telegram-topic-id-${index}`)
	});

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await telegramRow(page, 0).chatId.fill('123456789');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});

	test('loads, edits and saves telegram topic destinations', async ({ page }) => {
		const { getSavedPayloads, wasSaveEndpointCalled } = await setupNotificationTest(
			page,
			'telegram',
			{
				initialSettings: [telegramSettings(['@channel', '-1001234567890:42'])]
			}
		);

		await openProviderTab(page, 'Telegram');
		const saveButton = page.getByRole('button', { name: 'Save', exact: true });
		const panel = page.getByRole('tabpanel').filter({ visible: true });

		// Existing chats and chatId:topicId entries load into separate fields.
		await expect(telegramRow(page, 0).chatId).toHaveValue('@channel');
		await expect(telegramRow(page, 0).topicId).toHaveValue('');
		await expect(telegramRow(page, 1).chatId).toHaveValue('-1001234567890');
		await expect(telegramRow(page, 1).topicId).toHaveValue('42');
		await expect(saveButton).toBeDisabled();

		// A second topic in the same group plus a plain chat.
		await panel.getByRole('button', { name: 'Add destination' }).click();
		await telegramRow(page, 2).chatId.fill(' -1001234567890 ');
		await telegramRow(page, 2).topicId.fill(' 7 ');
		await panel.getByRole('button', { name: 'Add destination' }).click();
		await telegramRow(page, 3).chatId.fill('123456789');
		await expect(saveButton).toBeEnabled();

		// The redacted bot token stays blank and does not block saving.
		await expect(page.getByPlaceholder('123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11')).toHaveValue(
			''
		);
		await saveButton.click();
		await expect.poll(wasSaveEndpointCalled).toBe(true);
		expect(telegramChatIds(getSavedPayloads()[0])).toEqual([
			'@channel',
			'-1001234567890:42',
			'-1001234567890:7',
			'123456789'
		]);
		await expect(saveButton).toBeDisabled();

		// Saved destinations round-trip through a reload.
		await page.reload();
		await openProviderTab(page, 'Telegram');
		await expect(telegramRow(page, 3).chatId).toHaveValue('123456789');
		await expect(telegramRow(page, 2).topicId).toHaveValue('7');

		// Clearing a topic marks the form dirty and Reset restores the baseline.
		await telegramRow(page, 1).topicId.fill('');
		await expect(saveButton).toBeEnabled();
		await page.getByRole('button', { name: 'Reset', exact: true }).click();
		await expect(telegramRow(page, 1).topicId).toHaveValue('42');
		await expect(saveButton).toBeDisabled();

		// Removing a row saves the remaining destinations in order.
		await panel.getByRole('button', { name: 'Remove' }).nth(1).click();
		await expect(telegramRow(page, 1).topicId).toHaveValue('7');
		await saveButton.click();
		await expect.poll(() => getSavedPayloads().length).toBe(2);
		expect(telegramChatIds(getSavedPayloads()[1])).toEqual([
			'@channel',
			'-1001234567890:7',
			'123456789'
		]);
	});

	test('rejects invalid telegram destinations before saving or testing', async ({ page }) => {
		const { wasSaveEndpointCalled, wasTestEndpointCalled } = await setupNotificationTest(
			page,
			'telegram'
		);

		await openProviderTab(page, 'Telegram');
		await enableCurrentProvider(page);
		await page
			.getByPlaceholder('123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11')
			.fill('123456:TEST-TOKEN');

		const saveButton = page.getByRole('button', { name: 'Save', exact: true });
		const panel = page.getByRole('tabpanel').filter({ visible: true });

		// Non-numeric and non-positive topics are rejected.
		await telegramRow(page, 0).chatId.fill('-1001234567890');
		await telegramRow(page, 0).topicId.fill('abc');
		await expect(panel.getByText('Topic ID must be a positive whole number')).toBeVisible();
		await saveButton.click();
		await expect(page.getByText('Please check the form for errors').first()).toBeVisible();
		expect(wasSaveEndpointCalled()).toBe(false);

		await telegramRow(page, 0).topicId.fill('0');
		await expect(panel.getByText('Topic ID must be a positive whole number')).toBeVisible();

		// A topic without a chat is incomplete.
		await telegramRow(page, 0).topicId.fill('5');
		await telegramRow(page, 0).chatId.fill('');
		await expect(panel.getByText('This field is required')).toBeVisible();

		// A colon in the chat field is not accepted as an inline topic.
		await telegramRow(page, 0).chatId.fill('-1001234567890:5');
		await expect(panel.getByText(/Chat ID cannot contain a colon/)).toBeVisible();

		// Save & Test is blocked while the form is invalid.
		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();
		await page.getByRole('button', { name: 'Save & Test', exact: true }).click();
		await expect(page.getByText('Please check the form for errors').first()).toBeVisible();
		expect(wasSaveEndpointCalled()).toBe(false);
		expect(wasTestEndpointCalled()).toBe(false);

		// Fixing the row lets the save go through.
		await telegramRow(page, 0).chatId.fill('-1001234567890');
		await expect(panel.getByText(/Chat ID cannot contain a colon/)).toHaveCount(0);
		await saveButton.click();
		await expect.poll(wasSaveEndpointCalled).toBe(true);
	});

	test('should allow testing generic webhook notifications', async ({ page }) => {
		const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'generic');

		await openProviderTab(page, 'Generic');
		await enableCurrentProvider(page);

		await page.getByPlaceholder('https://example.com/webhook').fill('https://example.com/webhook');

		await openTestMenu(page);
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

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
		await page.getByRole('menuitem', { name: 'Simple', exact: true }).click();

		const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
		if (await saveAndTestButton.isVisible().catch(() => false)) {
			await saveAndTestButton.click();
		}

		await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
		getErrorCheck();
	});
});
