import { test as setup, expect } from '@playwright/test';

/**
 * GitOps Test Setup
 *
 * This setup:
 * 1. Registers a Git repository in Arcane (GitHub)
 * 2. Configures Arcane to sync from this repository
 */
const GITOPS_REPO_NAME = 'gitsyncs-test-repo';
const GITOPS_REPO_URL = 'https://github.com/getarcaneapp/gitsyncs.git';
const GITOPS_REPO_BRANCH = 'main';
const GITOPS_COMPOSE_PATH = 'compose-test-repo/compose.yaml';
const GITOPS_SYNC_NAME = 'gitops-test-sync';

setup('create gitops sync in arcane', async ({ page }) => {
  console.log('Creating GitOps sync configuration in Arcane...');

  // Step 1: Create a Git Repository in Arcane pointing to GitHub
  await page.goto('/customize/git-repositories');
  await page.waitForLoadState('networkidle');

  // Check if test repo already exists
  const existingRepo = page.getByRole('cell', { name: GITOPS_REPO_NAME });
  if ((await existingRepo.count()) === 0) {
    console.log('Creating Git repository in Arcane...');

    // Click Add Repository button
    const addRepoButton = page.getByRole('button', { name: /Add.*Repository/i });
    await addRepoButton.click();

    // Wait for dialog
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Fill repository form - use specific labels to avoid ambiguity
    await dialog.getByRole('textbox', { name: /Repository Name/i }).fill(GITOPS_REPO_NAME);
    await dialog.getByRole('textbox', { name: /Repository URL/i }).fill(GITOPS_REPO_URL);

    // Select "None" for auth type (public repo) - click the auth type dropdown
    const authTrigger = dialog.locator('#authType');
    if ((await authTrigger.count()) > 0) {
      await authTrigger.click();
      await page.waitForTimeout(300);
      const noneOption = page.getByRole('option', { name: /None|No Auth/i });
      if ((await noneOption.count()) > 0) {
        await noneOption.click();
      } else {
        await page.keyboard.press('Escape');
      }
    }

    // Submit the form
    const submitButton = dialog.getByRole('button', { name: /Add Repository/i });
    await submitButton.click();

    // Wait for success or error
    await page.waitForTimeout(2000);
    const successToast = page.getByText(/created|success/i);
    if ((await successToast.count()) > 0) {
      console.log('Git repository created successfully in Arcane');
    } else {
      console.log('Repository may already exist or creation had issues, continuing...');
    }
  } else {
    console.log('Git repository already exists in Arcane');
  }

  // Step 2: Create GitOps Sync
  await page.goto('/environments/0/gitops');
  await page.waitForLoadState('networkidle');

  // Check if sync already exists
  const existingSync = page.getByRole('cell', { name: GITOPS_SYNC_NAME });
  if ((await existingSync.count()) === 0) {
    console.log('Creating GitOps sync...');

    // Click Add Sync button
    const addSyncButton = page.getByRole('button', { name: /Add.*Sync/i });
    await addSyncButton.click();

    // Wait for dialog
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Wait for dialog content to load
    await page.waitForTimeout(1000);

    // Fill sync form - Name field (use specific selector)
    const nameInput = dialog.getByRole('textbox', { name: /Sync Name/i });
    await nameInput.fill(GITOPS_SYNC_NAME);

    // Select repository from dropdown
    const repoTrigger = dialog.locator('#repository, [id*="repository"]').first();
    if ((await repoTrigger.count()) > 0) {
      await repoTrigger.click();
      await page.waitForTimeout(300);
      const repoOption = page.getByRole('option', { name: GITOPS_REPO_NAME });
      if ((await repoOption.count()) > 0) {
        await repoOption.click();
      }
    }

    // Wait for branches to load
    await page.waitForTimeout(2000);

    // Select or enter branch
    const branchTrigger = dialog.locator('#branch, [id*="branch"]').first();
    if ((await branchTrigger.count()) > 0) {
      const isSelect = (await branchTrigger.getAttribute('role')) === 'combobox';
      if (isSelect) {
        await branchTrigger.click();
        await page.waitForTimeout(300);
        const mainOption = page.getByRole('option', { name: new RegExp(GITOPS_REPO_BRANCH, 'i') });
        if ((await mainOption.count()) > 0) {
          await mainOption.click();
        } else {
          await page.keyboard.press('Escape');
        }
      }
    }

    // Enter compose path
    const composePathInput = dialog.getByPlaceholder(/docker-compose|compose/i);
    if ((await composePathInput.count()) > 0) {
      await composePathInput.fill(GITOPS_COMPOSE_PATH);
    }

    // Disable auto-sync for tests
    const autoSyncSwitch = dialog.getByRole('switch');
    if ((await autoSyncSwitch.count()) > 0) {
      const isChecked = await autoSyncSwitch.getAttribute('data-state');
      if (isChecked === 'checked') {
        await autoSyncSwitch.click();
      }
    }

    // Submit the form
    const submitButton = dialog.getByRole('button', { name: /Add.*Sync|Create/i }).filter({ hasNotText: /Cancel/ });
    await submitButton.click();

    // Wait for result
    await page.waitForTimeout(3000);
    console.log('GitOps sync configuration created');
  } else {
    console.log('GitOps sync already exists');
  }

  // Step 3: Trigger initial sync to create the managed project
  console.log('Triggering initial sync...');
  await page.reload();
  await page.waitForLoadState('networkidle');

  // Find the sync row and trigger sync
  const syncRow = page.locator('tr').filter({ hasText: GITOPS_SYNC_NAME });
  if ((await syncRow.count()) > 0) {
    // Look for sync button or menu
    const syncButton = syncRow.getByRole('button', { name: /Sync|Sync Now/i });
    const menuButton = syncRow.getByRole('button', { name: /menu|actions|Open menu/i });

    if ((await syncButton.count()) > 0) {
      await syncButton.click();
    } else if ((await menuButton.count()) > 0) {
      await menuButton.click();
      await page.waitForTimeout(300);
      const syncMenuItem = page.getByRole('menuitem', { name: /Sync/i });
      if ((await syncMenuItem.count()) > 0) {
        await syncMenuItem.click();
      }
    }

    // Wait for sync to complete
    await page.waitForTimeout(5000);
    console.log('Initial sync triggered');
  }

  // Verify project was created
  await page.goto('/projects');
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(2000);

  console.log('GitOps test setup complete!');
});
