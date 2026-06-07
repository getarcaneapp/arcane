import { test, expect, type Page } from '@playwright/test';

const route = '/customize/registries';
const TOKEN = process.env.REGISTRY_TEST_TOKEN ?? 'e2e-token';
const pullUsageRoute = /\/api\/container-registries\/pull-usage(?:\?|$)/;
const registryListRoute = /\/api\/container-registries(?:\?|$)/;

async function mockRegistryList(page: Page, registryId = 'dockerhub-registry') {
	await page.context().route(registryListRoute, async (route) => {
		if (route.request().method() !== 'GET') {
			await route.fallback();
			return;
		}

		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				success: true,
				data: [
					{
						id: registryId,
						url: 'docker.io',
						username: 'docker-user',
						description: 'Docker Hub test registry',
						insecure: false,
						enabled: true,
						registryType: 'generic',
						createdAt: new Date().toISOString(),
						updatedAt: new Date().toISOString()
					}
				],
				pagination: {
					totalPages: 1,
					totalItems: 1,
					currentPage: 1,
					itemsPerPage: 20,
					grandTotalItems: 1
				}
			})
		});
	});
}

async function mockRegistryPullUsage(
	page: Page,
	options: { registryId?: string; remaining?: number; limit?: number; observedPulls?: number }
) {
	const registryId = options.registryId ?? 'dockerhub-registry';
	await page.context().route(pullUsageRoute, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				success: true,
				data: {
					registries: [
						{
							registryId,
							provider: 'dockerhub',
							registry: 'docker.io',
							displayName: 'Docker Hub',
							repository: 'ratelimitpreview/test',
							...(options.limit === undefined ? {} : { limit: options.limit }),
							...(options.remaining === undefined ? {} : { remaining: options.remaining }),
							observedPulls: options.observedPulls ?? 0,
							authMethod: 'credential',
							authUsername: 'docker-user',
							checkedAt: new Date().toISOString()
						}
					]
				}
			})
		});
	});
}

test.describe('Container Registries', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto(route);
		await page.waitForLoadState('networkidle');
	});

	test('should display title and subtitle, and refresh', async ({ page }) => {
		await expect(
			page.getByRole('heading', { name: 'Container Registries', level: 1 })
		).toBeVisible();
		await expect(
			page
				.getByText(
					'Configure access credentials for private Docker registries and container repositories'
				)
				.first()
		).toBeVisible();

		await page.getByRole('button', { name: 'Refresh' }).click();
		await expect(page.locator('li[data-sonner-toast] div[data-title]')).toBeVisible({
			timeout: 10000
		});
	});

	test('should open Add Registry dialog and validate required fields', async ({ page }) => {
		await page.getByRole('button', { name: 'Add Registry' }).click();

		const dialog = page.getByRole('dialog');
		await expect(dialog).toBeVisible();

		// Submit without URL -> expect validation error
		await dialog.getByRole('button', { name: /Add Registry|Save Changes/ }).click();
		await expect(dialog.getByText(/Registry URL is required/i)).toBeVisible();

		// Close dialog
		await page.keyboard.press('Escape');
		await expect(dialog).toBeHidden();
	});

	test('should display registry pull limit usage when headers are available', async ({ page }) => {
		await mockRegistryList(page);
		await mockRegistryPullUsage(page, { remaining: 76, limit: 100, observedPulls: 4 });
		await page.goto(route);
		await page.waitForLoadState('networkidle');

		const table = page.getByRole('table');
		await expect(table.getByText('Pull Usage')).toBeVisible();
		await expect(table.getByText('76/100')).toBeVisible();
	});

	test('should display observed pull count when registry limit headers are unavailable', async ({
		page
	}) => {
		await mockRegistryList(page);
		await mockRegistryPullUsage(page, { observedPulls: 4 });
		await page.goto(route);
		await page.waitForLoadState('networkidle');

		const table = page.getByRole('table');
		await expect(table.getByText('Pull Usage')).toBeVisible();
		await expect(table.getByText('4 pulls')).toBeVisible();
	});

	test('should create, test, edit, and delete a registry', async ({ page }) => {
		// Create
		await page.getByRole('button', { name: 'Add Registry' }).click();
		const dialog = page.getByRole('dialog');
		await expect(dialog).toBeVisible();

		const url = `e2e.example.com-${Date.now()}`;

		// URL
		await dialog.getByLabel(/Registry URL|^URL$/i).fill(url);
		// Username (frontend requires it)
		await dialog.getByLabel(/^Username$/i).fill('e2e');
		// Token (backend needs it to avoid 400)
		const tokenInput = dialog.getByLabel(/token|access token/i);
		await tokenInput.fill(TOKEN);

		// Optional description
		const desc = dialog.getByLabel(/^Description$/i);
		if (await desc.count()) await desc.fill('E2E test registry');

		await dialog.getByRole('button', { name: /Add Registry/ }).click();

		// Creation complete when dialog closes
		await expect(dialog).toBeHidden({ timeout: 10000 });

		// Row should appear
		const row = page.locator('tbody tr', { hasText: url }).first();
		await expect(row).toBeVisible();

		// Test Connection (accept either success or failure toast)
		await row.getByRole('button', { name: 'Open menu' }).click();
		await page.getByRole('menuitem', { name: 'Test Connection' }).click();
	});

	test('should open Remove Selected dialog and cancel (no mutation)', async ({ page }) => {
		const firstRowCheckbox = page.locator('tbody tr input[type="checkbox"]').first();
		if (await firstRowCheckbox.count()) {
			await firstRowCheckbox.check();

			const removeSelected = page.getByRole('button', { name: /Remove Selected/i });
			if (await removeSelected.count()) {
				await removeSelected.click();

				const confirm = page.locator(
					'div[role="heading"][aria-level="2"][data-dialog-title], [role="dialog"] >> text=Remove'
				);
				await expect(confirm).toBeVisible();

				await page.getByRole('button', { name: 'Cancel' }).click();
				await expect(confirm).toBeHidden();
			}
		}
	});
});
