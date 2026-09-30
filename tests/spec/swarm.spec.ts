import { test, expect } from '../fixtures/test.fixture';

test.describe('Swarm UI', () => {
	test('cluster page renders correct lifecycle controls for current swarm state', async ({
		page
	}) => {
		const swarmInfoResponsePromise = page.waitForResponse('**/api/environments/*/swarm/info');
		await page.goto('/swarm/cluster');
		await page.waitForLoadState('load');
		const swarmInfoResponse = await swarmInfoResponsePromise;

		await expect(page.getByRole('heading', { name: 'Cluster', level: 1 })).toBeVisible();

		const initializeCard = page
			.locator('[data-slot="card-title"]')
			.filter({ hasText: 'Initialize Cluster' });
		if (swarmInfoResponse.ok()) {
			await expect(page.getByRole('button', { name: 'Actions' })).toBeVisible();
			await expect(page.getByText('Initialize Cluster')).toHaveCount(0);
			await expect(page.getByText('Join Existing Cluster')).toHaveCount(0);
			await page.getByRole('button', { name: 'Actions' }).click();
			await expect(page.getByText('Unlock / Leave')).toBeVisible();
			await expect(page.getByRole('button', { name: 'Leave Cluster' })).toBeVisible();
		} else {
			await expect(initializeCard).toBeVisible();
			await expect(page.getByText('Join Existing Cluster')).toBeVisible();
			await expect(page.getByText('Choose how to set up this engine')).toBeVisible();
			await expect(
				page.locator('[data-slot="card-title"]').filter({ hasText: 'Cluster Status' })
			).toHaveCount(0);
			await expect(
				page.locator('[data-slot="card-title"]').filter({ hasText: 'Join Tokens' })
			).toHaveCount(0);
			await expect(
				page.locator('[data-slot="card-title"]').filter({ hasText: 'Update Swarm Spec' })
			).toHaveCount(0);

			await expect(page.getByPlaceholder('Remote manager addrs (comma separated)')).toBeVisible();
			await expect(page.getByPlaceholder('Listen address (optional)').first()).toBeHidden();
			await page.getByText('Advanced settings').first().click();
			await expect(page.getByPlaceholder('Listen address (optional)').first()).toBeVisible();
			await expect(page.getByRole('button', { name: 'Initialize' })).toBeVisible();
			await expect(page.getByRole('button', { name: 'Join' })).toBeVisible();
		}
	});
});
