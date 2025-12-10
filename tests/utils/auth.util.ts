import type { Page } from '@playwright/test';

async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Username').fill('arcane');
  await page.getByLabel('Password').fill('arcane-admin');
  await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
}

async function changeDefaultPassword(page: Page, newPassword: string) {
  const dialog = page.getByRole('dialog', { name: 'Change Default Password' });
  await dialog.waitFor({ state: 'visible' });
  await dialog.getByRole('textbox', { name: 'Current Password' }).fill('arcane-admin');
  await dialog.getByRole('textbox', { name: 'New Password', exact: true }).fill(newPassword);
  await dialog.getByRole('textbox', { name: 'Confirm New Password' }).fill(newPassword);
  await dialog.getByRole('button', { name: 'Change Password' }).click();
  await page.getByRole('listitem').filter({ hasText: 'Password changed successfully' }).waitFor({ state: 'visible' });
}

export default { login, changeDefaultPassword };
