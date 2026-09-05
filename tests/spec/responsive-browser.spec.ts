import { expect, test } from '../fixtures/test.fixture';

test('navigation matches the configured mobile or tablet viewport', async ({ page }, testInfo) => {
	await page.goto('/dashboard');

	if (testInfo.project.name === 'mobile-chromium') {
		await expect(page.getByTestId(/mobile-(floating|docked)-nav/)).toBeVisible();
		await expect(page.locator('[data-slot="sidebar"]')).toHaveCount(0);

		await page.getByTestId('mobile-nav-open').click();
		const navigationSheet = page.getByTestId('mobile-nav-sheet');
		await expect(navigationSheet).toBeVisible();
		await navigationSheet.getByRole('link', { name: 'Containers', exact: true }).click();
		await expect(page).toHaveURL('/containers');
		await expect(page.getByTestId(/mobile-(floating|docked)-nav/)).toBeVisible();
	} else {
		const sidebar = page.locator('[data-slot="sidebar"]');
		await expect(sidebar).toBeVisible();
		await expect(sidebar).toHaveAttribute('data-state', 'collapsed');
		await expect(page.getByTestId(/mobile-(floating|docked)-nav/)).toHaveCount(0);

		await page.locator('a[href="/containers"]').filter({ visible: true }).first().click();
		await expect(page).toHaveURL('/containers');
		await expect(sidebar).toHaveAttribute('data-state', 'collapsed');
	}

	expect(
		await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
		'The app shell must not introduce horizontal document overflow'
	).toBe(true);
});

test('resource dialog stays usable in the configured viewport', async ({ page }) => {
	await page.goto('/networks');
	const createButton = page
		.getByRole('button', { name: 'Create Network', exact: true })
		.filter({ visible: true });
	await expect(createButton).toBeInViewport();
	await createButton.click();

	const dialog = page.getByRole('dialog');
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('heading', { name: 'Create New Network' })).toBeVisible();
	await expect(dialog.getByLabel('Network Name *')).toBeInViewport();
	await expect(dialog.getByRole('button', { name: 'Create Network' })).toBeInViewport();

	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).toBeHidden();
});
