import {
	expect,
	formatPageErrors,
	installPageErrorCollector,
	test,
	type BrowserContext,
	type Page
} from '../fixtures/test.fixture';
import { readApiData } from '../utils/fetch.util';
import { openRowActionsMenu } from '../utils/table-actions.util';

type TestRole = {
	id: string;
	name: string;
	description?: string;
	permissions: string[];
	builtIn: boolean;
};

type TestUser = {
	id: string;
	username: string;
	displayName?: string;
	permissionsByEnv: Record<string, string[]>;
};

type TestEnvironment = {
	id: string;
	name: string;
	apiUrl: string;
	enabled: boolean;
};

async function selectPermission(page: Page, permission: string) {
	const search = page.getByPlaceholder('Filter permissions…');
	await search.fill(permission);
	const checkbox = page.locator(`[id="perm-${permission}"]`);
	await expect(checkbox).toBeVisible();
	await checkbox.click();
}

async function createRoleThroughUI(page: Page, name: string, permissions: string[]) {
	await page.goto('/settings/roles/new');
	await page.getByLabel('Name', { exact: true }).fill(name);
	await page
		.getByLabel('Description', { exact: true })
		.fill('Playwright environment-scoped reader');

	for (const permission of permissions) {
		await selectPermission(page, permission);
	}

	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/roles'
	);
	await page
		.locator('form')
		.getByRole('button', { name: /Create Role/ })
		.click();
	const role = await readApiData<TestRole>(await responsePromise, `Create role ${name}`);
	await expect(page).toHaveURL('/settings/roles');
	return role;
}

async function editRoleThroughUI(page: Page, role: TestRole) {
	await page.goto('/settings/roles');
	await page.getByPlaceholder('Search…').fill(role.name);
	const row = page.getByRole('row').filter({ hasText: role.name });
	const menu = await openRowActionsMenu(page, row);
	await menu.getByRole('menuitem', { name: 'Edit', exact: true }).click();

	await expect(page).toHaveURL(`/settings/roles/${role.id}`);
	await page
		.getByLabel('Description', { exact: true })
		.fill('Updated by the Playwright RBAC journey');

	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname === `/api/roles/${role.id}`
	);
	await page.getByRole('button', { name: 'Save changes', exact: true }).click();
	const updated = await readApiData<TestRole>(await responsePromise, `Update role ${role.name}`);
	await expect(page).toHaveURL('/settings/roles');
	expect(updated.description).toBe('Updated by the Playwright RBAC journey');
}

async function cloneViewerRoleThroughUI(page: Page) {
	await page.goto('/settings/roles/role_viewer');
	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/roles'
	);
	await page.getByRole('button', { name: 'Clone as custom role', exact: true }).click();
	const cloned = await readApiData<TestRole>(await responsePromise, 'Clone the Viewer role');
	await expect(page).toHaveURL(`/settings/roles/${cloned.id}`);
	expect(cloned.builtIn).toBe(false);
	expect(cloned.permissions.length).toBeGreaterThan(0);
	return cloned;
}

async function selectUserAssignment(
	page: Page,
	dialog: ReturnType<Page['getByRole']>,
	index: number,
	environmentName: string,
	roleName: string
) {
	const environmentSelect = dialog
		.getByRole('button', { name: 'Environment', exact: true })
		.nth(index);
	await environmentSelect.click();
	await page.getByRole('option', { name: environmentName, exact: true }).click();

	const roleSelect = dialog.getByRole('button', { name: 'Role', exact: true }).nth(index);
	await roleSelect.click();
	await page.getByRole('option').filter({ hasText: roleName }).click();
}

async function createRestrictedUserThroughUI(
	page: Page,
	username: string,
	password: string,
	localEnvironmentName: string,
	localRoleName: string,
	remoteEnvironmentName: string,
	remoteRoleName: string
) {
	await page.goto('/settings/users');
	await page.getByRole('button', { name: 'Create User', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Create New User' });
	await expect(dialog).toBeVisible();

	await dialog.getByLabel('Username', { exact: true }).fill(username);
	await dialog.getByLabel('Password *', { exact: true }).fill(password);
	await dialog.getByLabel('Display Name', { exact: true }).fill('Scoped Browser User');
	await dialog.getByLabel('Email', { exact: true }).fill(`${username}@example.test`);

	await dialog.getByRole('button', { name: 'Add assignment', exact: true }).click();
	await selectUserAssignment(page, dialog, 0, localEnvironmentName, localRoleName);
	await dialog.getByRole('button', { name: 'Add assignment', exact: true }).click();
	await selectUserAssignment(page, dialog, 1, remoteEnvironmentName, remoteRoleName);

	const createResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/users'
	);
	const assignmentsResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			/^\/api\/users\/[^/]+\/role-assignments$/.test(new URL(response.url()).pathname)
	);
	await dialog.getByRole('button', { name: 'Create User', exact: true }).click();

	const user = await readApiData<TestUser>(await createResponsePromise, `Create user ${username}`);
	await readApiData<unknown[]>(await assignmentsResponsePromise, `Assign roles to ${username}`);
	await expect(dialog).toBeHidden();
	return user;
}

async function editUserThroughUI(page: Page, user: TestUser) {
	await page.goto('/settings/users');
	await page.getByPlaceholder('Search…').fill(user.username);
	const row = page.getByRole('row').filter({ hasText: user.username });
	const menu = await openRowActionsMenu(page, row);
	await menu.getByRole('menuitem', { name: 'Edit', exact: true }).click();

	const dialog = page.getByRole('dialog', { name: 'Edit User' });
	await expect(dialog).toBeVisible();
	await dialog.getByLabel('Display Name', { exact: true }).fill('Scoped Browser User Updated');

	const updateResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname === `/api/users/${user.id}`
	);
	const assignmentsResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname === `/api/users/${user.id}/role-assignments`
	);
	await dialog.getByRole('button', { name: 'Save Changes', exact: true }).click();

	const updated = await readApiData<TestUser>(
		await updateResponsePromise,
		`Update user ${user.username}`
	);
	await readApiData<unknown[]>(
		await assignmentsResponsePromise,
		`Retain roles for ${user.username}`
	);
	await expect(dialog).toBeHidden();
	expect(updated.displayName).toBe('Scoped Browser User Updated');
}

async function loginAs(page: Page, username: string, password: string, expectedPath: string) {
	await page.goto('/login');
	await page.getByLabel('Username').fill(username);
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
	await expect(page).toHaveURL(expectedPath, { timeout: 15_000 });
}

async function deleteUserThroughUI(page: Page, user: TestUser) {
	await page.goto('/settings/users');
	await page.getByPlaceholder('Search…').fill(user.username);
	const row = page.getByRole('row').filter({ hasText: user.username });
	const menu = await openRowActionsMenu(page, row);
	await menu.getByRole('menuitem', { name: 'Delete', exact: true }).click();

	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'DELETE' &&
			new URL(response.url()).pathname === `/api/users/${user.id}`
	);
	await page.getByRole('dialog').getByRole('button', { name: 'Delete', exact: true }).click();
	await readApiData<{ message: string }>(await responsePromise, `Delete user ${user.username}`);
	await expect(row).toHaveCount(0);
}

async function deleteRoleThroughUI(page: Page, role: TestRole) {
	await page.goto('/settings/roles');
	await page.getByPlaceholder('Search…').fill(role.name);
	const row = page.getByRole('row').filter({ hasText: role.name });
	const menu = await openRowActionsMenu(page, row);
	await menu.getByRole('menuitem', { name: 'Delete', exact: true }).click();

	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'DELETE' &&
			new URL(response.url()).pathname === `/api/roles/${role.id}`
	);
	await page.getByRole('dialog').getByRole('button', { name: 'Delete', exact: true }).click();
	await readApiData<{ message: string }>(await responsePromise, `Delete role ${role.name}`);
	await expect(row).toHaveCount(0);
}

test('administers scoped identities and enforces their browser access immediately', async ({
	browser,
	page
}, testInfo) => {
	test.setTimeout(180_000);
	page.setDefaultTimeout(10_000);
	page.setDefaultNavigationTimeout(15_000);

	const suffix = Date.now().toString(36);
	const localRoleName = `E2E Container Reader ${suffix}`;
	const remoteRoleName = `E2E Project Reader ${suffix}`;
	const remoteEnvironmentName = `E2E Scoped Remote ${suffix}`;
	const username = `e2e-scoped-${suffix}`;
	const noAccessUsername = `e2e-no-access-${suffix}`;
	const password = 'E2e-RBAC-user-123!';

	let localRole: TestRole | null = null;
	let remoteRole: TestRole | null = null;
	let clonedRole: TestRole | null = null;
	let remoteEnvironment: TestEnvironment | null = null;
	let restrictedUser: TestUser | null = null;
	let noAccessUser: TestUser | null = null;
	let restrictedContext: BrowserContext | null = null;
	let noAccessContext: BrowserContext | null = null;

	try {
		const localEnvironment = await readApiData<TestEnvironment>(
			await page.request.get('/api/environments/0'),
			'Get local environment'
		);

		localRole = await createRoleThroughUI(page, localRoleName, [
			'containers:list',
			'containers:read'
		]);
		await editRoleThroughUI(page, localRole);
		clonedRole = await cloneViewerRoleThroughUI(page);

		remoteRole = await readApiData<TestRole>(
			await page.request.post('/api/roles', {
				data: {
					name: remoteRoleName,
					description: 'Playwright remote project reader',
					permissions: ['projects:list', 'projects:read']
				}
			}),
			`Create role ${remoteRoleName}`
		);

		remoteEnvironment = await readApiData<TestEnvironment>(
			await page.request.post('/api/environments', {
				data: {
					name: remoteEnvironmentName,
					apiUrl: 'http://rbac-remote.invalid:3552',
					enabled: true,
					isEdge: false
				}
			}),
			`Create environment ${remoteEnvironmentName}`
		);

		restrictedUser = await createRestrictedUserThroughUI(
			page,
			username,
			password,
			localEnvironment.name,
			localRole.name,
			remoteEnvironment.name,
			remoteRole.name
		);
		await editUserThroughUI(page, restrictedUser);

		noAccessUser = await readApiData<TestUser>(
			await page.request.post('/api/users', {
				data: {
					username: noAccessUsername,
					password,
					displayName: 'No Access Browser User'
				}
			}),
			`Create user ${noAccessUsername}`
		);

		const baseURL = String(testInfo.project.use.baseURL);
		restrictedContext = await browser.newContext({
			baseURL,
			storageState: { cookies: [], origins: [] }
		});
		const remoteID = remoteEnvironment.id;
		await restrictedContext.route(`**/api/environments/${remoteID}/projects**`, async (route) => {
			const pathname = new URL(route.request().url()).pathname;
			if (pathname.endsWith('/counts')) {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						success: true,
						data: {
							runningProjects: 0,
							stoppedProjects: 0,
							totalProjects: 0,
							archivedProjects: 0
						}
					})
				});
				return;
			}

			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					success: true,
					data: [],
					counts: {
						runningProjects: 0,
						stoppedProjects: 0,
						totalProjects: 0,
						archivedProjects: 0
					},
					pagination: {
						currentPage: 1,
						totalPages: 0,
						totalItems: 0,
						itemsPerPage: 20
					}
				})
			});
		});
		await restrictedContext.route(`**/api/environments/${remoteID}/settings`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ success: true, data: {} })
			});
		});

		const restrictedPage = await restrictedContext.newPage();
		restrictedPage.setDefaultTimeout(10_000);
		const restrictedErrors = installPageErrorCollector(restrictedPage);
		try {
			await loginAs(restrictedPage, username, password, '/containers');

			await expect(
				restrictedPage.getByRole('link', { name: 'Containers', exact: true })
			).toBeVisible();
			await expect(restrictedPage.getByRole('link', { name: 'Projects', exact: true })).toHaveCount(
				0
			);
			await expect(
				restrictedPage.getByRole('link', { name: 'Dashboard', exact: true })
			).toHaveCount(0);
			await expect(restrictedPage.getByRole('link', { name: 'Settings', exact: true })).toHaveCount(
				0
			);
			await expect(
				restrictedPage.getByRole('button', { name: 'Create Container', exact: true })
			).toHaveCount(0);

			const containerRow = restrictedPage
				.getByRole('row')
				.filter({ has: restrictedPage.getByRole('button', { name: 'Open menu', exact: true }) })
				.first();
			const containerMenu = await openRowActionsMenu(restrictedPage, containerRow);
			await expect(
				containerMenu.getByRole('menuitem', { name: 'Inspect', exact: true })
			).toBeVisible();
			for (const action of ['Start', 'Stop', 'Restart', 'Remove']) {
				await expect(
					containerMenu.getByRole('menuitem', { name: action, exact: true })
				).toHaveCount(0);
			}
			await restrictedPage.keyboard.press('Escape');

			await restrictedPage.goto('/settings/backups');
			await expect(restrictedPage).toHaveURL('/containers');
			await restrictedPage.goto('/projects');
			await expect(restrictedPage).toHaveURL('/containers');

			const currentUser = await readApiData<TestUser>(
				await restrictedPage.request.get('/api/auth/me'),
				'Get restricted current user'
			);
			expect(currentUser.permissionsByEnv['0']).toEqual(
				expect.arrayContaining(['containers:list', 'containers:read'])
			);
			expect(currentUser.permissionsByEnv[remoteID]).toEqual(
				expect.arrayContaining(['projects:list', 'projects:read'])
			);
			expect(currentUser.permissionsByEnv.global ?? []).toEqual([]);

			await restrictedPage
				.getByRole('button')
				.filter({ hasText: localEnvironment.name })
				.first()
				.click();
			const environmentDialog = restrictedPage.getByRole('dialog', {
				name: 'Select Environment'
			});
			await expect(environmentDialog).toBeVisible();
			await environmentDialog
				.getByRole('button')
				.filter({ hasText: remoteEnvironment.name })
				.first()
				.click();

			await expect(restrictedPage).toHaveURL('/projects');
			await expect(
				restrictedPage.getByRole('link', { name: 'Projects', exact: true })
			).toBeVisible();
			await expect(
				restrictedPage.getByRole('link', { name: 'Containers', exact: true })
			).toHaveCount(0);
		} finally {
			restrictedErrors.stop();
			expect(
				restrictedErrors.errors,
				`Restricted user page errors:\n${formatPageErrors(restrictedErrors.errors)}`
			).toEqual([]);
		}

		noAccessContext = await browser.newContext({
			baseURL,
			storageState: { cookies: [], origins: [] }
		});
		const noAccessPage = await noAccessContext.newPage();
		noAccessPage.setDefaultTimeout(10_000);
		const noAccessErrors = installPageErrorCollector(noAccessPage);
		try {
			await loginAs(noAccessPage, noAccessUsername, password, '/no-access');
			await expect(
				noAccessPage.getByRole('heading', { name: "You don't have access to anything yet" })
			).toBeVisible();
			await expect(noAccessPage.getByRole('link')).toHaveCount(0);
		} finally {
			noAccessErrors.stop();
			expect(
				noAccessErrors.errors,
				`No-access user page errors:\n${formatPageErrors(noAccessErrors.errors)}`
			).toEqual([]);
		}

		await restrictedContext.close();
		restrictedContext = null;
		await noAccessContext.close();
		noAccessContext = null;

		await deleteUserThroughUI(page, restrictedUser);
		restrictedUser = null;
		await deleteRoleThroughUI(page, clonedRole);
		clonedRole = null;
		await deleteRoleThroughUI(page, localRole);
		localRole = null;
	} finally {
		await restrictedContext?.close();
		await noAccessContext?.close();

		if (restrictedUser) {
			await page.request.delete(`/api/users/${restrictedUser.id}`).catch(() => undefined);
		}
		if (noAccessUser) {
			await page.request.delete(`/api/users/${noAccessUser.id}`).catch(() => undefined);
		}
		for (const role of [clonedRole, localRole, remoteRole]) {
			if (role) {
				await page.request.delete(`/api/roles/${role.id}`).catch(() => undefined);
			}
		}
		if (remoteEnvironment) {
			await page.request.delete(`/api/environments/${remoteEnvironment.id}`).catch(() => undefined);
		}
	}
});
