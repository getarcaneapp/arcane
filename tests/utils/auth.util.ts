import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { expect, type Page } from '@playwright/test';
import { readApiData } from './fetch.util';
import { waitForDialogReady } from './playwright.util';
import { reportDirectory } from './report';

const TEST_PASSWORD = 'Test-password-123';
const GENERATED_PASSWORD_FILE = 'initial-admin-password';

function generatedAdminPassword(): string {
	const composeFile = process.env.ARCANE_E2E_COMPOSE_FILE;
	if (!composeFile) throw new Error('ARCANE_E2E_COMPOSE_FILE is not set');
	const target = path.join(reportDirectory, GENERATED_PASSWORD_FILE);
	fs.mkdirSync(reportDirectory, { recursive: true });
	execFileSync(
		'docker',
		['compose', '-f', composeFile, 'cp', `arcane:/app/data/${GENERATED_PASSWORD_FILE}`, target],
		{ stdio: 'pipe' }
	);
	return fs.readFileSync(target, 'utf8').trim();
}

async function login(page: Page): Promise<string> {
	await page.goto('/login');
	await page.getByLabel('Username').fill('arcane');

	for (const readPassword of [() => TEST_PASSWORD, generatedAdminPassword]) {
		const password = readPassword();
		await page.getByLabel('Password').fill(password);
		const responsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === '/api/auth/login'
		);
		await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
		const response = await responsePromise;
		if (response.status() === 401) {
			await expect(
				page.getByRole('alert').filter({ hasText: 'Invalid username or password' })
			).toBeVisible();
			continue;
		}
		expect(response.ok(), `Login failed: ${response.status()}`).toBe(true);
		await page.waitForURL('/dashboard');
		return password;
	}

	throw new Error('Unable to authenticate with known E2E credentials');
}

async function changeDefaultPassword(page: Page, currentPassword: string, newPassword: string) {
	const dialog = page.getByRole('dialog', { name: 'Change Default Password' });

	const user = await readApiData<{ requiresPasswordChange: boolean }>(
		await page.request.get('/api/auth/me'),
		'Get current user'
	);
	if (!user.requiresPasswordChange) return;

	await waitForDialogReady(dialog);
	await dialog.getByRole('textbox', { name: 'Current Password' }).fill(currentPassword);
	await dialog.getByRole('textbox', { name: 'New Password', exact: true }).fill('abcdefghijkl');
	await dialog.getByRole('textbox', { name: 'Confirm New Password' }).fill('abcdefghijkl');
	const submitButton = dialog.getByRole('button', { name: 'Change Password' });
	await submitButton.click();
	await expect(dialog.getByRole('alert')).toContainText(
		'12+ chars with upper, lower, number, and symbol.'
	);
	const policyResponse = await page.request.post('/api/auth/password', {
		data: { currentPassword, newPassword: 'abcdefghijkl' }
	});
	expect(policyResponse.status()).toBe(400);
	expect(await policyResponse.json()).toMatchObject({
		status: 400,
		type: 'urn:arcane:problem:password-policy:strong'
	});
	await expect(dialog.locator('#new-password-requirements')).toHaveText(
		'12+ chars with upper, lower, number, and symbol.'
	);

	await dialog.getByRole('textbox', { name: 'New Password', exact: true }).fill(newPassword);
	await dialog.getByRole('textbox', { name: 'Confirm New Password' }).fill(newPassword);
	await expect(submitButton).toBeEnabled();
	await submitButton.click();
	await page
		.getByRole('listitem')
		.filter({ hasText: 'Password changed successfully' })
		.waitFor({ state: 'visible' });
}

export default { login, changeDefaultPassword, TEST_PASSWORD };
