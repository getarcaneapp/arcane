import { test, expect, type Page } from '@playwright/test';

test.describe('Notification settings', () => {
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

    // Stub "save settings"
    await page.route('**/api/environments/*/notifications/settings', async (route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        saveEndpointCalled = true;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true }),
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
        body: JSON.stringify({ success: true }),
      });
    });

    await page.goto('/settings/notifications');
    await page.waitForLoadState('networkidle');

    return {
      getErrorCheck: () => {
        const stateUnsafe = observedErrors.filter((e) => e.includes('state_unsafe_mutation'));
        expect(stateUnsafe, `Unexpected state_unsafe_mutation errors: ${stateUnsafe.join('\n')}`).toHaveLength(0);
      },
      wasTestEndpointCalled: () => testEndpointCalled,
      wasSaveEndpointCalled: () => saveEndpointCalled,
    };
  };

  test('should allow testing email notifications without state_unsafe_mutation errors', async ({ page }) => {
    const { getErrorCheck, wasTestEndpointCalled } = await setupNotificationTest(page, 'email');

    // Enable email notifications
    await page.locator('#email-enabled').click();

    // Fill fields
    await page.getByPlaceholder('smtp.example.com').fill('smtp.example.com');
    await page.getByPlaceholder('notifications@example.com').fill('notifications@example.com');
    await page.getByPlaceholder('user1@example.com, user2@example.com').fill('user1@example.com');

    // Trigger test
    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test' }).click();
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

    await page.getByRole('tab', { name: 'Discord' }).click();
    await page.locator('#discord-enabled').click();

    // Discord split fields
    await page.getByPlaceholder('Enter webhook ID').fill('123456789');
    await page.getByPlaceholder('Enter webhook token').fill('abc-def-ghi');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
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

    await page.getByRole('tab', { name: 'Slack' }).click();
    await page.locator('#slack-enabled').click();

    // Slack OAuth token (xoxb- or xoxp- format)
    await page.getByPlaceholder('xoxb-... or xoxp-...').fill('xoxb-123456789012-1234567890123-abcdefghijklmnopqrstuvwx');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
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

    await page.getByRole('tab', { name: 'Telegram' }).click();
    await page.locator('#telegram-enabled').click();

    // Telegram fields (placeholders are hardcoded in component)
    await page.getByPlaceholder('123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11').fill('123456:TEST-TOKEN');
    await page.getByPlaceholder('@channel, 123456789, @another_channel').fill('123456789');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
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

    await page.getByRole('tab', { name: 'Generic' }).click();
    await page.locator('#generic-enabled').click();

    await page.getByPlaceholder('https://example.com/webhook').fill('https://example.com/webhook');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
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

    await page.getByRole('tab', { name: 'Signal' }).click();
    await page.locator('#signal-enabled').click();

    await page.getByPlaceholder('localhost').fill('signal-api.example.com');
    await page.getByPlaceholder('8080').fill('8080');
    await page.locator('#signal-source').fill('+1234567890');
    await page.locator('#signal-recipients').fill('+1987654321');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
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

    await page.getByRole('tab', { name: 'Ntfy' }).click();
    await page.locator('#ntfy-enabled').click();

    await page.getByPlaceholder('ntfy.sh').fill('ntfy.sh');
    await page.getByPlaceholder('my-updates').fill('arcane-updates');

    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Provider' }).click();
    await page.getByRole('menuitem', { name: 'Simple Test Notification', exact: true }).click();

    const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
    if (await saveAndTestButton.isVisible().catch(() => false)) {
      await saveAndTestButton.click();
    }

    await expect.poll(wasTestEndpointCalled, { timeout: 10_000 }).toBe(true);
    getErrorCheck();
  });
});
