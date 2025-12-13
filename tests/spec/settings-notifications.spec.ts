import { test, expect } from '@playwright/test';

test.describe('Notification settings', () => {
  test('should allow testing email notifications without state_unsafe_mutation errors', async ({ page }) => {
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

    // Stub "save settings" so we don't depend on a real backend/DB for this flow.
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

    // Stub the test endpoint so no SMTP server is required.
    await page.route('**/api/environments/*/notifications/test/email**', async (route) => {
      testEndpointCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true }),
      });
    });

    await page.goto('/settings/notifications');
    await page.waitForLoadState('networkidle');

    // Enable email notifications (the test button only appears when enabled).
    await page.locator('#email-enabled').click();

    // Fill the minimum required fields so "Save and Test" is valid.
    await page.getByPlaceholder('smtp.example.com').fill('smtp.example.com');
    await page.getByPlaceholder('notifications@example.com').fill('notifications@example.com');
    await page.getByPlaceholder('user1@example.com, user2@example.com').fill('user1@example.com');

    // The dropdown trigger uses an internal button wrapper; target the trigger attribute to avoid strict-mode ambiguity.
    await page.locator('[data-dropdown-menu-trigger]').filter({ hasText: 'Test Email' }).click();
    await page.getByRole('menuitem', { name: 'Simple Test Email', exact: true }).click();

    // If we have unsaved changes, the page will prompt to save before testing.
    const saveAndTestButton = page.getByRole('button', { name: 'Save & Test', exact: true });
    const didClickSaveAndTest = await saveAndTestButton.isVisible().catch(() => false);
    if (didClickSaveAndTest) {
      await saveAndTestButton.click();
    }

    await expect.poll(() => testEndpointCalled, { timeout: 10_000 }).toBe(true);

    // If the modal path was taken, we should have attempted to save.
    // (Not strictly required for this bug, but it's a helpful sanity check.)
    if (didClickSaveAndTest) {
      await expect.poll(() => saveEndpointCalled, { timeout: 10_000 }).toBe(true);
    }

    const stateUnsafe = observedErrors.filter((e) => e.includes('state_unsafe_mutation'));
    expect(stateUnsafe, `Unexpected state_unsafe_mutation errors: ${stateUnsafe.join('\n')}`).toHaveLength(0);
  });
});
