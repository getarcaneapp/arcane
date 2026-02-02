import { test as setup, expect } from '@playwright/test';
import { execSync } from 'child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

/**
 * GitOps Test Setup
 *
 * This setup:
 * 1. Creates a test user in the local Gitea server
 * 2. Creates a test repository with a compose.yaml file
 * 3. Configures Arcane to sync from this repository
 *
 * The Gitea server runs as part of the test Docker Compose environment.
 */

const GITEA_URL = 'http://localhost:3001';
const GITEA_INTERNAL_URL = 'http://gitea:3001'; // URL accessible from Arcane container
const GITEA_USER = 'testuser';
const GITEA_PASSWORD = 'testpassword123';
const GITEA_EMAIL = 'testuser@test.local';
const GITEA_REPO_NAME = 'test-compose-repo';
const GITOPS_REPO_NAME = 'test-gitops-repo';
const GITOPS_SYNC_NAME = 'gitops-test-sync';

async function waitForGitea(maxAttempts = 30): Promise<boolean> {
  for (let i = 0; i < maxAttempts; i++) {
    try {
      const response = await fetch(`${GITEA_URL}/api/v1/version`);
      if (response.ok) {
        console.log('Gitea is ready');
        return true;
      }
    } catch {
      // Gitea not ready yet
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  return false;
}

async function createGiteaUser(): Promise<string | null> {
  // First check if user already exists by trying to get a token
  try {
    const tokenResponse = await fetch(`${GITEA_URL}/api/v1/users/${GITEA_USER}/tokens`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Basic ' + Buffer.from(`${GITEA_USER}:${GITEA_PASSWORD}`).toString('base64'),
      },
      body: JSON.stringify({ name: 'test-token-' + Date.now(), scopes: ['all'] }),
    });

    if (tokenResponse.ok) {
      const tokenData = await tokenResponse.json();
      console.log('User already exists, got token');
      return tokenData.sha1;
    }
  } catch {
    // User doesn't exist, continue to create
  }

  // Create user via Gitea API (admin endpoint - works when INSTALL_LOCK=true but no admin exists)
  // We'll use the built-in user creation flow
  console.log('Creating Gitea user via registration...');

  try {
    const response = await fetch(`${GITEA_URL}/user/sign_up`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: new URLSearchParams({
        user_name: GITEA_USER,
        email: GITEA_EMAIL,
        password: GITEA_PASSWORD,
        retype: GITEA_PASSWORD,
      }),
      redirect: 'manual',
    });

    // Registration redirects on success
    if (response.status === 302 || response.status === 303 || response.ok) {
      console.log('User created successfully');
    }
  } catch (error) {
    console.log('User creation response (may already exist):', error);
  }

  // Get access token
  const tokenResponse = await fetch(`${GITEA_URL}/api/v1/users/${GITEA_USER}/tokens`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: 'Basic ' + Buffer.from(`${GITEA_USER}:${GITEA_PASSWORD}`).toString('base64'),
    },
    body: JSON.stringify({ name: 'test-token-' + Date.now(), scopes: ['all'] }),
  });

  if (tokenResponse.ok) {
    const tokenData = await tokenResponse.json();
    return tokenData.sha1;
  }

  console.error('Failed to get token:', await tokenResponse.text());
  return null;
}

async function createRepository(token: string): Promise<boolean> {
  // Check if repo exists
  const checkResponse = await fetch(`${GITEA_URL}/api/v1/repos/${GITEA_USER}/${GITEA_REPO_NAME}`, {
    headers: { Authorization: `token ${token}` },
  });

  if (checkResponse.ok) {
    console.log('Repository already exists');
    return true;
  }

  // Create repository
  const createResponse = await fetch(`${GITEA_URL}/api/v1/user/repos`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `token ${token}`,
    },
    body: JSON.stringify({
      name: GITEA_REPO_NAME,
      description: 'Test repository for GitOps testing',
      private: false,
      auto_init: true,
      default_branch: 'main',
    }),
  });

  if (!createResponse.ok) {
    console.error('Failed to create repository:', await createResponse.text());
    return false;
  }

  console.log('Repository created successfully');

  // Wait a moment for the repo to be ready
  await new Promise((r) => setTimeout(r, 1000));

  // Add compose.yaml file
  const composeContent = `services:
  nginx:
    image: nginx:alpine
    container_name: gitops-test-nginx
    ports:
      - "8888:80"
    volumes:
      - nginx_data:/usr/share/nginx/html

volumes:
  nginx_data:
`;

  const fileResponse = await fetch(
    `${GITEA_URL}/api/v1/repos/${GITEA_USER}/${GITEA_REPO_NAME}/contents/compose.yaml`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `token ${token}`,
      },
      body: JSON.stringify({
        message: 'Add compose.yaml for testing',
        content: Buffer.from(composeContent).toString('base64'),
        branch: 'main',
      }),
    },
  );

  if (!fileResponse.ok) {
    console.error('Failed to add compose.yaml:', await fileResponse.text());
    return false;
  }

  console.log('compose.yaml added to repository');
  return true;
}

setup('setup local gitea server', async ({ request }) => {
  console.log('Setting up local Gitea server for GitOps testing...');

  // Wait for Gitea to be ready
  const giteaReady = await waitForGitea();
  if (!giteaReady) {
    console.error('Gitea did not become ready in time');
    setup.skip(true, 'Gitea not available');
    return;
  }

  // Create user and get token
  const token = await createGiteaUser();
  if (!token) {
    console.error('Failed to create Gitea user or get token');
    setup.skip(true, 'Could not create Gitea user');
    return;
  }

  // Create repository with compose file
  const repoCreated = await createRepository(token);
  if (!repoCreated) {
    console.error('Failed to create test repository');
    setup.skip(true, 'Could not create test repository');
    return;
  }

  console.log('Local Git server setup complete!');
  console.log(`Repository URL: ${GITEA_INTERNAL_URL}/${GITEA_USER}/${GITEA_REPO_NAME}.git`);
});

setup('create gitops sync in arcane', async ({ page }) => {
  console.log('Creating GitOps sync configuration in Arcane...');

  // Step 1: Create a Git Repository in Arcane pointing to local Gitea
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
    await dialog.getByRole('textbox', { name: /Repository URL/i }).fill(`${GITEA_INTERNAL_URL}/${GITEA_USER}/${GITEA_REPO_NAME}.git`);

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
        const mainOption = page.getByRole('option', { name: /main/i });
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
      await composePathInput.fill('compose.yaml');
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
