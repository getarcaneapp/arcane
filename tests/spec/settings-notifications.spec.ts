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
          body: JSON.stringify({ id: '1', name: 'My Email', provider: 'email', enabled: true, config: {} }),
        });
        return;
      }
      if (req.method() === 'GET') {
        if (saveEndpointCalled) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify([{ id: '1', name: 'My Email', provider: 'email', enabled: true, config: {} }]),
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify([]),
          });
        }
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

    // Click "Add Provider"
    await page.getByRole('button', { name: /Add Provider/i }).click();

    // Select "Email" from the provider dropdown
    await page.getByLabel(/Type/i).click();
    await page.getByRole('option', { name: /Email/i }).click();

    // Fill in the fields
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('My Email');
    await page.getByPlaceholder('smtp.gmail.com').fill('smtp.example.com');
    await page.getByPlaceholder('arcane@example.com').fill('notifications@example.com');
    await page.locator('input[id="toAddresses"]').fill('user1@example.com');

    // Click "Add"
    await page.getByRole('button', { name: /Add/i }).click();

    // Wait for the table to update
    await expect(page.getByText('My Email')).toBeVisible();

    // Open the menu for the new provider
    await page.getByLabel(/Open menu/i).click();

    // Click "Test"
    await page.getByRole('menuitem', { name: /Test/i }).click();

    await expect.poll(() => testEndpointCalled, { timeout: 10_000 }).toBe(true);

    const stateUnsafe = observedErrors.filter((e) => e.includes('state_unsafe_mutation'));
    expect(stateUnsafe, `Unexpected state_unsafe_mutation errors: ${stateUnsafe.join('\n')}`).toHaveLength(0);
  });
});
