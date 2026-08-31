import { fileURLToPath } from 'node:url';
import {
	expect,
	formatPageErrors,
	installPageErrorCollector,
	test,
	type BrowserContext,
	type Page
} from '../fixtures/test.fixture';
import { readApiData } from '../utils/fetch.util';

const AVATAR_PATH = fileURLToPath(
	new URL('../../backend/resources/images/icon-128x128.png', import.meta.url)
);

type UserPreferences = {
	themeMode?: 'light' | 'dark' | 'system';
	keyboardShortcutsEnabled?: boolean;
	mobileNavigationMode?: 'floating' | 'docked';
	defaultLandingPage?: string;
};

type AccountUser = {
	id: string;
	username: string;
	displayName?: string;
	email?: string;
	avatarUrl?: string;
	timeFormat: 'auto' | '12h' | '24h';
	preferences?: UserPreferences;
};

type PersonalApiKey = {
	id: string;
	name: string;
	key: string;
};

type Passkey = {
	id: string;
	name: string;
};

async function login(page: Page, username: string, password: string, expectedPath = '/dashboard') {
	await page.goto('/login');
	await page.getByLabel('Username', { exact: true }).fill(username);
	await page.getByLabel('Password', { exact: true }).fill(password);
	await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
	await expect(page).toHaveURL(expectedPath);
	await expect(page.locator('main').first()).toBeVisible();
}

async function waitForProfileUpdate(page: Page, action: () => Promise<void>) {
	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname === '/api/auth/me/profile'
	);
	await action();
	return responsePromise;
}

test('manages an isolated account, preferences, credentials, and sessions', async ({
	browser,
	page
}, testInfo) => {
	test.setTimeout(180_000);

	const suffix = Date.now().toString(36);
	const username = `e2e-account-${suffix}`;
	const initialPassword = 'E2e-account-initial-123!';
	const updatedPassword = 'E2e-account-updated-456!';
	const displayName = `Account Journey ${suffix}`;
	const email = `${username}@example.test`;
	const keyName = `account-key-${suffix}`;
	let user: AccountUser | null = null;
	let accountContext: BrowserContext | null = null;

	try {
		user = await readApiData<AccountUser>(
			await page.request.post('/api/users', {
				data: {
					username,
					password: initialPassword,
					displayName: 'Account Journey Initial',
					email: `${username}-initial@example.test`
				}
			}),
			`Create account user ${username}`
		);
		await readApiData<unknown[]>(
			await page.request.put(`/api/users/${user.id}/role-assignments`, {
				data: { assignments: [{ roleId: 'role_viewer' }] }
			}),
			`Assign Viewer role to ${username}`
		);

		accountContext = await browser.newContext({
			baseURL: String(testInfo.project.use.baseURL),
			storageState: { cookies: [], origins: [] },
			extraHTTPHeaders: { 'X-Forwarded-For': '198.18.0.1' }
		});
		const accountPage = await accountContext.newPage();
		accountPage.setDefaultTimeout(12_000);
		const pageErrors = installPageErrorCollector(accountPage);

		try {
			await login(accountPage, username, initialPassword);
			await accountPage.goto('/account');
			await expect(accountPage.getByRole('heading', { name: 'Account', level: 1 })).toBeVisible();

			await accountPage.getByLabel('Display Name', { exact: true }).fill(displayName);
			await accountPage.getByLabel('Email', { exact: true }).fill(email);
			const profileResponse = await waitForProfileUpdate(accountPage, () =>
				accountPage.getByRole('button', { name: 'Save profile', exact: true }).click()
			);
			const updatedProfile = await readApiData<AccountUser>(
				profileResponse,
				'Update account profile'
			);
			expect(updatedProfile).toMatchObject({ displayName, email });

			await accountPage.getByLabel('Display Name', { exact: true }).fill('Unsaved account name');
			await accountPage.getByRole('button', { name: 'Reset', exact: true }).click();
			await expect(accountPage.getByLabel('Display Name', { exact: true })).toHaveValue(
				displayName
			);

			await accountPage.locator('#account-avatar-cropper').setInputFiles(AVATAR_PATH);
			const cropDialog = accountPage
				.getByRole('dialog')
				.filter({ has: accountPage.getByRole('heading', { name: 'Crop profile picture' }) });
			await expect(cropDialog).toBeVisible();
			const avatarUploadResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/me/avatar'
			);
			await cropDialog.getByRole('button', { name: 'Crop photo', exact: true }).click();
			const avatarUser = await readApiData<AccountUser>(
				await avatarUploadResponsePromise,
				'Upload account avatar'
			);
			expect(avatarUser.avatarUrl).toBeTruthy();

			const profileSection = accountPage
				.locator('section')
				.filter({ has: accountPage.getByRole('heading', { name: 'Profile', exact: true }) });
			const avatarDeleteResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'DELETE' &&
					new URL(response.url()).pathname === '/api/auth/me/avatar'
			);
			await profileSection.getByRole('button', { name: 'Remove', exact: true }).click();
			const avatarRemovedUser = await readApiData<AccountUser>(
				await avatarDeleteResponsePromise,
				'Remove account avatar'
			);
			expect(avatarRemovedUser.avatarUrl).toBeFalsy();

			await accountPage.getByRole('button', { name: 'New key', exact: true }).click();
			const keyDialog = accountPage.getByRole('dialog', { name: 'Create API Key' });
			await keyDialog.getByLabel('Name', { exact: true }).fill(keyName);
			await keyDialog.getByLabel('Description', { exact: true }).fill('Account browser journey');
			const keyCreateResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/me/api-keys'
			);
			await keyDialog.getByRole('button', { name: 'Create API Key', exact: true }).click();
			const createdKey = await readApiData<PersonalApiKey>(
				await keyCreateResponsePromise,
				'Create personal API key'
			);
			expect(createdKey.name).toBe(keyName);
			expect(createdKey.key).toBeTruthy();
			await accountPage.getByRole('button', { name: "I've saved it", exact: true }).click();

			const keyRow = accountPage.locator('li').filter({ hasText: keyName });
			await expect(keyRow).toBeVisible();
			await keyRow.getByRole('button', { name: 'Delete', exact: true }).click();
			const keyDeleteResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'DELETE' &&
					new URL(response.url()).pathname === `/api/auth/me/api-keys/${createdKey.id}`
			);
			await accountPage
				.getByRole('dialog')
				.getByRole('button', { name: 'Delete', exact: true })
				.click();
			expect((await keyDeleteResponsePromise).ok()).toBe(true);
			await expect(keyRow).toHaveCount(0);

			await accountPage.getByRole('tab', { name: 'Preferences', exact: true }).click();
			await expect(accountPage).toHaveURL('/account?tab=preferences');
			const original = await readApiData<AccountUser>(
				await accountPage.request.get('/api/auth/me'),
				'Get account preferences'
			);

			const timeFormat = original.timeFormat === '24h' ? '12h' : '24h';
			const timeFormatLabel = timeFormat === '24h' ? '24-hour' : '12-hour';
			const timeFormatResponse = await waitForProfileUpdate(accountPage, async () => {
				await accountPage.locator('#accountTimeFormatPicker').click();
				await accountPage.getByRole('option', { name: timeFormatLabel, exact: true }).click();
			});
			expect(timeFormatResponse.request().postDataJSON()).toEqual({ timeFormat });

			const themeMode = original.preferences?.themeMode === 'dark' ? 'light' : 'dark';
			const themeLabel = themeMode === 'dark' ? 'Dark' : 'Light';
			const themeResponse = await waitForProfileUpdate(accountPage, () =>
				accountPage.getByRole('button', { name: themeLabel, exact: true }).click()
			);
			expect(themeResponse.request().postDataJSON()).toEqual({ preferences: { themeMode } });

			const landingPage =
				original.preferences?.defaultLandingPage === '/containers' ? '/dashboard' : '/containers';
			const landingLabel = landingPage === '/containers' ? 'Containers' : 'Dashboard';
			const landingResponse = await waitForProfileUpdate(accountPage, async () => {
				await accountPage.locator('#account-default-landing-page').click();
				await accountPage.getByRole('option', { name: landingLabel, exact: true }).click();
			});
			expect(landingResponse.request().postDataJSON()).toEqual({
				preferences: { defaultLandingPage: landingPage }
			});

			const keyboardShortcutsEnabled = !(original.preferences?.keyboardShortcutsEnabled ?? true);
			const keyboardResponse = await waitForProfileUpdate(accountPage, () =>
				accountPage.locator('#account-keyboard-shortcuts').click()
			);
			expect(keyboardResponse.request().postDataJSON()).toEqual({
				preferences: { keyboardShortcutsEnabled }
			});

			const mobileNavigationMode =
				original.preferences?.mobileNavigationMode === 'docked' ? 'floating' : 'docked';
			const mobileNavigationLabel = mobileNavigationMode === 'docked' ? 'Docked' : 'Floating';
			const mobileNavigationResponse = await waitForProfileUpdate(accountPage, () =>
				accountPage.getByRole('button', { name: mobileNavigationLabel, exact: true }).click()
			);
			expect(mobileNavigationResponse.request().postDataJSON()).toEqual({
				preferences: { mobileNavigationMode }
			});

			const persisted = await readApiData<AccountUser>(
				await accountPage.request.get('/api/auth/me'),
				'Verify persisted account preferences'
			);
			expect(persisted).toMatchObject({
				timeFormat,
				preferences: {
					themeMode,
					defaultLandingPage: landingPage,
					keyboardShortcutsEnabled,
					mobileNavigationMode
				}
			});

			await accountPage.reload();
			await expect(accountPage.locator('#account-keyboard-shortcuts')).toHaveAttribute(
				'data-state',
				keyboardShortcutsEnabled ? 'checked' : 'unchecked'
			);
			await expect(
				accountPage.getByRole('button', { name: mobileNavigationLabel, exact: true })
			).toHaveAttribute('aria-pressed', 'true');

			await accountPage.getByRole('tab', { name: 'Account', exact: true }).click();
			const logoutOtherResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/sessions/logout-all'
			);
			await accountPage
				.getByRole('button', { name: 'Sign out other sessions', exact: true })
				.click();
			expect((await logoutOtherResponsePromise).ok()).toBe(true);

			await accountPage.getByLabel('Current password', { exact: true }).fill(initialPassword);
			await accountPage.getByLabel('New password', { exact: true }).fill(updatedPassword);
			await accountPage.getByLabel('Confirm new password', { exact: true }).fill(updatedPassword);
			const passwordResponsePromise = accountPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/password'
			);
			await accountPage.getByRole('button', { name: 'Update password', exact: true }).click();
			expect((await passwordResponsePromise).ok()).toBe(true);

			const dangerZone = accountPage
				.locator('section')
				.filter({ has: accountPage.getByRole('heading', { name: 'Danger zone', exact: true }) });
			await dangerZone.getByRole('button', { name: 'Log out', exact: true }).click();
			await expect(accountPage).toHaveURL('/login');
			await login(accountPage, username, updatedPassword, landingPage);
		} finally {
			pageErrors.stop();
			expect(
				pageErrors.errors,
				`Account journey page errors:\n${formatPageErrors(pageErrors.errors)}`
			).toEqual([]);
		}
	} finally {
		await accountContext?.close();
		if (user) {
			await page.request.delete(`/api/users/${user.id}`).catch(() => undefined);
		}
	}
});

test('registers, renames, authenticates with, and removes a passkey with MFA', async ({
	browser,
	page
}, testInfo) => {
	test.setTimeout(180_000);

	const suffix = Date.now().toString(36);
	const username = `e2e-passkey-${suffix}`;
	const password = 'E2e-passkey-user-123!';
	const renamedPasskey = `Virtual authenticator ${suffix}`;
	let user: AccountUser | null = null;
	let passkeyContext: BrowserContext | null = null;

	try {
		user = await readApiData<AccountUser>(
			await page.request.post('/api/users', {
				data: {
					username,
					password,
					displayName: 'Passkey Browser User',
					email: `${username}@example.test`
				}
			}),
			`Create passkey user ${username}`
		);
		await readApiData<unknown[]>(
			await page.request.put(`/api/users/${user.id}/role-assignments`, {
				data: { assignments: [{ roleId: 'role_viewer' }] }
			}),
			`Assign Viewer role to ${username}`
		);

		const webAuthnURL = new URL(String(testInfo.project.use.baseURL));
		webAuthnURL.hostname = 'localhost';
		passkeyContext = await browser.newContext({
			baseURL: webAuthnURL.origin,
			storageState: { cookies: [], origins: [] },
			extraHTTPHeaders: { 'X-Forwarded-For': '198.18.0.1' }
		});
		const passkeyPage = await passkeyContext.newPage();
		passkeyPage.setDefaultTimeout(15_000);
		const cdp = await passkeyContext.newCDPSession(passkeyPage);
		await cdp.send('WebAuthn.enable');
		const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
			options: {
				protocol: 'ctap2',
				transport: 'internal',
				hasResidentKey: true,
				hasUserVerification: true,
				isUserVerified: true,
				automaticPresenceSimulation: true
			}
		});
		const pageErrors = installPageErrorCollector(passkeyPage);

		try {
			await login(passkeyPage, username, password);
			await passkeyPage.goto('/account');
			const addPasskeyButton = passkeyPage.getByRole('button', {
				name: 'Add passkey',
				exact: true
			});
			await expect(addPasskeyButton).toBeEnabled();
			const registrationResponsePromise = passkeyPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/me/passkeys/register/finish'
			);
			await addPasskeyButton.click();
			const registered = await readApiData<Passkey>(
				await registrationResponsePromise,
				'Register virtual passkey'
			);
			const passkeyRow = passkeyPage.locator('li').filter({ hasText: registered.name });
			await expect(passkeyRow).toBeVisible();

			await passkeyRow.getByRole('button', { name: 'Rename', exact: true }).click();
			await passkeyPage.getByRole('textbox', { name: 'Name', exact: true }).fill(renamedPasskey);
			await passkeyPage.getByRole('button', { name: 'Save', exact: true }).click();
			const stepUpDialog = passkeyPage.getByRole('dialog', { name: 'Confirm your identity' });
			await expect(stepUpDialog).toBeVisible();
			await stepUpDialog
				.getByPlaceholder('Enter your current password', { exact: true })
				.fill(password);
			const renameResponsePromise = passkeyPage.waitForResponse(
				(response) =>
					response.request().method() === 'PUT' &&
					new URL(response.url()).pathname === `/api/auth/me/passkeys/${registered.id}`
			);
			await stepUpDialog
				.getByRole('button', { name: 'Continue with password', exact: true })
				.click();
			const renamed = await readApiData<Passkey>(
				await renameResponsePromise,
				'Rename virtual passkey'
			);
			expect(renamed.name).toBe(renamedPasskey);
			const renamedRow = passkeyPage.locator('li').filter({ hasText: renamedPasskey });
			await expect(renamedRow).toBeVisible();

			await passkeyPage.getByRole('button', { name: 'Enable MFA', exact: true }).click();
			const mfaDialog = passkeyPage.getByRole('dialog', { name: 'Passkey MFA' });
			await expect(mfaDialog).toBeVisible();
			const mfaEnableResponsePromise = passkeyPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/me/mfa/enable'
			);
			await mfaDialog.getByRole('button', { name: 'Enable MFA', exact: true }).click();
			const recovery = await readApiData<{ codes: string[] }>(
				await mfaEnableResponsePromise,
				'Enable passkey MFA'
			);
			expect(recovery.codes).toHaveLength(10);
			const recoveryCodes = passkeyPage
				.getByRole('heading', { name: 'Recovery codes', exact: true })
				.locator('..')
				.getByRole('listitem');
			await expect(recoveryCodes).toHaveCount(10);
			await passkeyPage.getByRole('button', { name: "I've saved it", exact: true }).click();

			const dangerZone = passkeyPage
				.locator('section')
				.filter({ has: passkeyPage.getByRole('heading', { name: 'Danger zone', exact: true }) });
			await dangerZone.getByRole('button', { name: 'Log out', exact: true }).click();
			await expect(passkeyPage).toHaveURL('/login');

			await passkeyPage.getByRole('button', { name: 'Passkey', exact: true }).click();
			const usePasskeyButton = passkeyPage.getByRole('button', {
				name: 'Use passkey',
				exact: true
			});
			if (await usePasskeyButton.isVisible().catch(() => false)) {
				await usePasskeyButton.click();
			}
			await expect(passkeyPage).toHaveURL('/dashboard');
			await expect(passkeyPage.locator('main').first()).toBeVisible();

			await passkeyPage.goto('/account');
			await passkeyPage.getByRole('button', { name: 'Disable MFA', exact: true }).click();
			const disableDialog = passkeyPage.getByRole('dialog', { name: 'Disable passkey MFA' });
			await expect(disableDialog).toBeVisible();
			await disableDialog.getByRole('button', { name: 'Disable MFA', exact: true }).click();
			const disableStepUp = passkeyPage.getByRole('dialog', { name: 'Confirm your identity' });
			await expect(disableStepUp).toBeVisible();
			const mfaDisableResponsePromise = passkeyPage.waitForResponse(
				(response) =>
					response.request().method() === 'POST' &&
					new URL(response.url()).pathname === '/api/auth/me/mfa/disable'
			);
			await disableStepUp
				.getByRole('button', { name: 'Continue with passkey', exact: true })
				.click();
			expect((await mfaDisableResponsePromise).ok()).toBe(true);
			await expect(
				passkeyPage.getByRole('button', { name: 'Enable MFA', exact: true })
			).toBeVisible();

			const finalPasskeyRow = passkeyPage.locator('li').filter({ hasText: renamedPasskey });
			await finalPasskeyRow.getByRole('button', { name: 'Delete', exact: true }).click();
			const deleteDialog = passkeyPage.getByRole('dialog', { name: 'Delete passkey' });
			const passkeyDeleteResponsePromise = passkeyPage.waitForResponse(
				(response) =>
					response.request().method() === 'DELETE' &&
					new URL(response.url()).pathname === `/api/auth/me/passkeys/${registered.id}`
			);
			await deleteDialog.getByRole('button', { name: 'Delete', exact: true }).click();
			expect((await passkeyDeleteResponsePromise).ok()).toBe(true);
			await expect(finalPasskeyRow).toHaveCount(0);
		} finally {
			pageErrors.stop();
			expect(
				pageErrors.errors,
				`Passkey journey page errors:\n${formatPageErrors(pageErrors.errors)}`
			).toEqual([]);
			await cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
			await cdp.send('WebAuthn.disable');
		}
	} finally {
		await passkeyContext?.close();
		if (user) {
			await page.request.delete(`/api/users/${user.id}`).catch(() => undefined);
		}
	}
});
