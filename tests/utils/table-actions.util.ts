import { expect, type Locator, type Page } from '@playwright/test';

export async function openRowActionsMenu(page: Page, row: Locator) {
	const targetRow = row.first();
	await expect(targetRow).toBeVisible();
	await targetRow.hover();

	const trigger = targetRow.getByRole('button', { name: 'Open menu', exact: true }).first();
	await expect(trigger).toBeVisible();
	await trigger.click();

	const menu = page.getByRole('menu').filter({ visible: true }).last();
	await expect(menu).toBeVisible();
	return menu;
}
