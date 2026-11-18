import { expect, type Locator, type Page } from '@playwright/test';

export const ROW_ACTIONS_TRIGGER_NAME = /^(Open menu|Actions for .+)$/;

export async function openRowActionsMenu(page: Page, row: Locator) {
	const targetRow = row.first();
	await expect(targetRow).toBeVisible();

	const trigger = targetRow.getByRole('button', { name: ROW_ACTIONS_TRIGGER_NAME }).first();
	await expect(trigger).toBeVisible();
	await expect(async () => {
		await targetRow.hover();
		await trigger.click({ timeout: 1_000 });
	}).toPass();

	const menu = page.getByRole('menu').filter({ visible: true }).last();
	await expect(menu).toBeVisible();
	return menu;
}

export async function sortByColumn(scope: Page | Locator, name: string, direction: 'asc' | 'desc') {
	const header = scope.getByRole('columnheader', { name: `Sort by ${name}`, exact: true });
	const button = header.getByRole('button', { name: `Sort by ${name}`, exact: true });
	const target = direction === 'asc' ? 'ascending' : 'descending';
	await expect(async () => {
		if ((await header.getAttribute('aria-sort')) !== target) await button.click();
		await expect(header).toHaveAttribute('aria-sort', target, { timeout: 1_000 });
	}).toPass();
}
