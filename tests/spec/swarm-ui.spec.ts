import { test, expect, type Page } from '@playwright/test';

async function mockConfigs(page: Page) {
	await page.route('**/api/environments/*/swarm/configs', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				success: true,
				data: []
			})
		});
	});
}

async function mockSecrets(page: Page) {
	await page.route('**/api/environments/*/swarm/secrets', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				success: true,
				data: []
			})
		});
	});
}

test.describe('Swarm UI', () => {
	test('cluster page renders correct lifecycle controls for current swarm state', async ({
		page
	}) => {
		await page.goto('/swarm/cluster');
		await page.waitForLoadState('networkidle');

		await expect(page.getByRole('heading', { name: 'Cluster', level: 1 })).toBeVisible();

		const initializeCard = page
			.locator('[data-slot="card-title"]')
			.filter({ hasText: 'Initialize Cluster' });
		if ((await initializeCard.count()) === 0) {
			await expect(page.getByRole('button', { name: 'Actions' })).toBeVisible();
			await expect(page.getByText('Initialize Cluster')).toHaveCount(0);
			await expect(page.getByText('Join Existing Cluster')).toHaveCount(0);
			await page.getByRole('button', { name: 'Actions' }).click();
			await expect(page.getByText('Unlock / Leave')).toBeVisible();
			await expect(page.getByRole('button', { name: 'Leave Cluster' })).toBeVisible();
		} else {
			await expect(initializeCard).toBeVisible();
			await expect(page.getByText('Join Existing Cluster')).toBeVisible();
			await expect(page.getByPlaceholder('Listen address (optional)').first()).toBeVisible();
			await expect(page.getByRole('button', { name: 'Initialize' })).toBeVisible();
			await expect(page.getByRole('button', { name: 'Join' })).toBeVisible();
		}
	});

	test('configs page renders name/data fields and empty state', async ({ page }) => {
		await mockConfigs(page);
		await page.goto('/swarm/configs');
		await page.waitForLoadState('networkidle');

		await expect(page.getByRole('heading', { name: 'Configs', level: 1 })).toBeVisible();
		await expect(
			page.locator('[data-slot="card-title"]').filter({ hasText: 'Create Config' })
		).toBeVisible();
		await expect(page.getByPlaceholder('Config name')).toBeVisible();
		await expect(page.getByPlaceholder('Config data')).toBeVisible();
		await expect(page.getByRole('button', { name: 'Create Config' })).toBeVisible();
		await expect(page.getByText('No configs found.')).toBeVisible();
	});

	test('secrets page renders name/data fields and empty state', async ({ page }) => {
		await mockSecrets(page);
		await page.goto('/swarm/secrets');
		await page.waitForLoadState('networkidle');

		await expect(page.getByRole('heading', { name: 'Secrets', level: 1 })).toBeVisible();
		await expect(
			page.locator('[data-slot="card-title"]').filter({ hasText: 'Create Secret' })
		).toBeVisible();
		await expect(page.getByPlaceholder('Secret name')).toBeVisible();
		await expect(page.getByPlaceholder('Secret data')).toBeVisible();
		await expect(page.getByRole('button', { name: 'Create Secret' })).toBeVisible();
		await expect(page.getByText('No secrets found.')).toBeVisible();
	});
});
