import type { Page } from '@playwright/test';

const DEFAULT_PASSWORD = 'arcane-admin';
const TEST_PASSWORD = 'test-password-123';

async function login(page: Page): Promise<string> {
	await page.goto('/login');
	await page.getByLabel('Username').fill('arcane');

	for (const password of [DEFAULT_PASSWORD, TEST_PASSWORD]) {
		await page.getByLabel('Password').fill(password);
		await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();

		try {
			await page.waitForURL('/dashboard', { timeout: 5000 });
			return password;
		} catch {
			const invalidCredentialsAlert = page
				.getByRole('alert')
				.filter({ hasText: 'Invalid username or password' });
			if (await invalidCredentialsAlert.isVisible().catch(() => false)) {
				continue;
			}
		}
	}

	throw new Error('Unable to authenticate with known E2E credentials');
}

async function changeDefaultPassword(page: Page, currentPassword: string, newPassword: string) {
	const dialog = page.getByRole('dialog', { name: 'Change Default Password' });

	if (!(await dialog.isVisible().catch(() => false))) {
		return;
	}

	await dialog.waitFor({ state: 'visible' });
	await dialog.getByRole('textbox', { name: 'Current Password' }).fill(currentPassword);
	await dialog.getByRole('textbox', { name: 'New Password', exact: true }).fill(newPassword);
	await dialog.getByRole('textbox', { name: 'Confirm New Password' }).fill(newPassword);
	await dialog.getByRole('button', { name: 'Change Password' }).click();
	await page
		.getByRole('listitem')
		.filter({ hasText: 'Password changed successfully' })
		.waitFor({ state: 'visible' });
}

export default { login, changeDefaultPassword, DEFAULT_PASSWORD, TEST_PASSWORD };
