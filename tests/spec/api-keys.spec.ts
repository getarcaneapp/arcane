import { test, expect, type Page } from '@playwright/test';
import { createTestApiKeys, deleteTestApiKeys } from '../utils/playwright.util';

const API_KEYS_ROUTE = '/settings/api-keys';

async function navigateToApiKeys(page: Page) {
  await page.goto(API_KEYS_ROUTE);
  await page.waitForLoadState('networkidle');
}

test.describe('API Keys Page', () => {
  // Create test API keys before tests that need existing data
  test.beforeAll(async () => {
    await createTestApiKeys(3);
  });

  // Clean up test API keys after all tests
  test.afterAll(async () => {
    await deleteTestApiKeys();
  });

  test('should display the API keys page title and description', async ({ page }) => {
    await navigateToApiKeys(page);
    await expect(page.getByRole('heading', { name: 'API Keys', level: 1 })).toBeVisible();
    await expect(page.getByText('Manage API keys for programmatic access to Arcane').first()).toBeVisible();
  });

  test('should open Create API Key sheet', async ({ page }) => {
    await navigateToApiKeys(page);

    await page.getByRole('button', { name: 'Create API Key' }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Create API Key').first()).toBeVisible();
  });

  test('should validate required fields when creating API key', async ({ page }) => {
    await navigateToApiKeys(page);

    await page.getByRole('button', { name: 'Create API Key' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Try to submit without filling required name field
    await dialog.getByRole('button', { name: /Create API Key/i }).click();

    // Should show validation error for name field
    await expect(dialog.getByText(/is required/i)).toBeVisible();
  });

  test('should create a new API key and show the key dialog', async ({ page }) => {
    await navigateToApiKeys(page);

    await page.getByRole('button', { name: 'Create API Key' }).click();
    const createDialog = page.getByRole('dialog');
    await expect(createDialog).toBeVisible();

    const apiKeyName = `test-key-${Date.now()}`;

    // Fill in the name field
    await createDialog.getByLabel(/Name/i).fill(apiKeyName);

    // Optionally fill description
    const descInput = createDialog.getByLabel(/Description/i);
    if (await descInput.count()) {
      await descInput.fill('E2E test API key');
    }

    // Submit the form
    await createDialog.getByRole('button', { name: /Create API Key/i }).click();

    // Should show success toast
    await expect(page.locator('li[data-sonner-toast][data-type="success"]').first()).toBeVisible({ timeout: 10000 });

    // Should show the API key reveal dialog
    await expect(page.getByText('API Key Created')).toBeVisible();
    await expect(page.getByText(/Copy your API key now/i)).toBeVisible();

    // The key should be visible in a code/snippet element
    await expect(page.locator('code, [data-snippet]').first()).toBeVisible();

    // Close the dialog
    await page.getByRole('button', { name: 'Done' }).click();

    // The new key should appear in the table
    await expect(page.locator(`tr:has-text("${apiKeyName}")`)).toBeVisible();
  });

  test('should open edit dialog from row actions', async ({ page }) => {
    await navigateToApiKeys(page);

    const firstRow = page.locator('tbody tr').first();
    await expect(firstRow).toBeVisible();

    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Edit' }).click();

    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Edit API Key')).toBeVisible();

    // Close the dialog
    await page.keyboard.press('Escape');
  });

  test('should open delete confirmation dialog from row actions', async ({ page }) => {
    await navigateToApiKeys(page);

    const firstRow = page.locator('tbody tr').first();
    await expect(firstRow).toBeVisible();

    await firstRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();

    // Should show confirmation dialog
    await expect(page.getByText(/Delete API Key/i)).toBeVisible();
    await expect(page.getByText(/Are you sure/i)).toBeVisible();

    // Cancel the deletion
    await page.getByRole('button', { name: 'Cancel' }).click();
  });

  test('should delete an API key', async ({ page }) => {
    // First create a key to delete
    await navigateToApiKeys(page);

    await page.getByRole('button', { name: 'Create API Key' }).click();
    const createDialog = page.getByRole('dialog');

    const apiKeyName = `delete-test-${Date.now()}`;
    await createDialog.getByLabel(/Name/i).fill(apiKeyName);
    await createDialog.getByRole('button', { name: /Create API Key/i }).click();

    // Wait for creation success and close reveal dialog
    await expect(page.getByText('API Key Created')).toBeVisible({ timeout: 10000 });
    await page.getByRole('button', { name: 'Done' }).click();

    // Wait for toasts to clear
    await page.waitForTimeout(1000);

    // Now delete the key
    const keyRow = page.locator(`tr:has-text("${apiKeyName}")`);
    await expect(keyRow).toBeVisible();

    await keyRow.getByRole('button', { name: 'Open menu' }).click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();

    // Confirm deletion
    await expect(page.getByText(/Delete API Key/i)).toBeVisible();
    await page.getByRole('button', { name: 'Delete' }).click();

    // Should show success toast for deletion
    await expect(page.getByText(/deleted successfully/i)).toBeVisible({ timeout: 10000 });

    // Key should no longer be in the table
    await expect(keyRow).toBeHidden();
  });

  test('should select multiple API keys and show bulk delete option', async ({ page }) => {
    await navigateToApiKeys(page);

    // Select first two checkboxes
    const checkboxes = page.locator('tbody tr input[type="checkbox"]');
    const checkboxCount = await checkboxes.count();

    if (checkboxCount < 2) {
      test.skip(true, 'Need at least 2 API keys for bulk selection test');
      return;
    }

    await checkboxes.nth(0).check();
    await checkboxes.nth(1).check();

    // Remove Selected button should appear
    const removeSelectedBtn = page.getByRole('button', { name: /Remove Selected/i });
    await expect(removeSelectedBtn).toBeVisible();

    // Click it to open confirmation
    await removeSelectedBtn.click();

    // Should show bulk delete confirmation
    await expect(page.getByText(/Delete.*API Key/i)).toBeVisible();

    // Cancel
    await page.getByRole('button', { name: 'Cancel' }).click();
  });

  test('should display correct status badges for active keys', async ({ page }) => {
    await navigateToApiKeys(page);

    // Check if Active badge is visible
    await expect(page.locator('text="Active"').first()).toBeVisible();
  });

  test('should display "Never" for keys without expiration', async ({ page }) => {
    await navigateToApiKeys(page);

    // Test keys created without expiration should show "Never"
    await expect(page.locator('text="Never"').first()).toBeVisible();
  });
});
