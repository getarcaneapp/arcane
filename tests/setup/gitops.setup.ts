import { expect, test as setup } from '../fixtures/test.fixture';

setup('create and verify the GitOps project prerequisite', async ({ page }) => {
	setup.setTimeout(120_000);
	const response = await page.request.post('/api/playwright/create-test-gitops-project', {
		timeout: 100_000
	});
	expect(response.status(), await response.text()).toBe(204);
	await page.goto('/projects');
	await expect(page.getByRole('link', { name: 'gitops-test-project', exact: true })).toBeVisible();
});
