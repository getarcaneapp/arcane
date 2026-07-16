import { expect, test } from '@playwright/test';

test.describe('Operations', () => {
	test('should display environment attention and recent activity', async ({ page }) => {
		await page.goto('/operations');

		await expect(page.getByRole('heading', { name: 'Operations', level: 1 })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'Needs attention', level: 2 })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'Recent activity', level: 2 })).toBeVisible();
	});

	test('should display the unified workload updates table', async ({ page }) => {
		await page.goto('/operations/updates');

		await expect(page.getByRole('heading', { name: 'Workload updates', level: 1 })).toBeVisible();
		await expect(page.getByRole('table')).toBeVisible();
		await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
		await expect(page.getByRole('columnheader', { name: 'Type' })).toBeVisible();
		await expect(page.getByRole('columnheader', { name: 'Environments' })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Check Updates', exact: true })).toBeVisible();
	});
});
