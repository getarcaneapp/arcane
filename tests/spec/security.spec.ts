import { test, expect, type Page } from '@playwright/test';

const ROUTES = {
	page: '/security',
	legacyVulnerabilities: '/images/vulnerabilities'
};

async function navigateToSecurity(page: Page) {
	await page.goto(ROUTES.page);
	await page.waitForLoadState('load');
}

test.describe('Security Page', () => {
	test('redirects the old vulnerabilities URL to the security page', async ({ page }) => {
		await page.goto(ROUTES.legacyVulnerabilities);
		await page.waitForURL('**/security**');
		await expect(page.getByRole('heading', { name: 'Security', level: 1 })).toBeVisible();
	});

	test('shows the vulnerabilities and patches tabs', async ({ page }) => {
		await navigateToSecurity(page);

		await expect(page.getByRole('tab', { name: 'Vulnerabilities', exact: true })).toHaveAttribute(
			'data-state',
			'active'
		);
		await expect(page.getByRole('tab', { name: 'Patches', exact: true })).toBeVisible();
	});

	test('loads ignored vulnerabilities via the table switch', async ({ page }) => {
		await navigateToSecurity(page);

		const ignoredResponse = page.waitForResponse((response) => {
			const request = response.request();
			const url = new URL(response.url());
			return (
				request.method() === 'GET' &&
				url.pathname.endsWith('/vulnerabilities/all') &&
				url.searchParams.get('ignored') === 'true'
			);
		});

		await page.getByRole('switch', { name: 'Show ignored' }).click();
		const response = await ignoredResponse;
		expect(response.ok()).toBeTruthy();
		await expect(page.getByRole('switch', { name: 'Show ignored' })).toBeChecked();
	});
});
