import {
	expect,
	formatPageErrors,
	installPageErrorCollector,
	test,
	type BrowserContext,
	type Page,
	type Request,
	type Response
} from '../fixtures/test.fixture';
import { TEST_COMPOSE_YAML } from '../setup/project.data';
import { removeApiResource, readApiData } from '../utils/fetch.util';
import { openRowActionsMenu } from '../utils/table-actions.util';
import { setCodeMirrorValue } from '../utils/playwright.util';

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

	let createRequests = 0;
	const countCreateRequests = (request: Request) => {
		if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/users') {
			createRequests += 1;
		}
	};
	page.on('request', countCreateRequests);

	await dialog.getByLabel('Username', { exact: true }).fill('   ');
	await dialog.getByLabel('Password *', { exact: true }).fill('short');
	await dialog.getByLabel('Display Name', { exact: true }).fill('x'.repeat(256));
	await dialog.getByRole('button', { name: 'Create User', exact: true }).click();
	await expect(dialog.getByText('Username is required', { exact: true })).toBeVisible();
	await expect(
		dialog.getByText('Password must be at least 8 characters', { exact: true })
	).toBeVisible();
	await expect(
		dialog.getByText('Display Name must be 255 characters or fewer', { exact: true })
	).toBeVisible();
	await expect.poll(() => createRequests).toBe(0);

	await dialog.getByLabel('Username', { exact: true }).fill('x'.repeat(256));
	await dialog.getByLabel('Password *', { exact: true }).fill(password);
	await dialog.getByLabel('Display Name', { exact: true }).fill('Scoped Browser User');
	await dialog.getByRole('button', { name: 'Create User', exact: true }).click();
	await expect(
		dialog.getByText('Username must be 255 characters or fewer', { exact: true })
	).toBeVisible();
	await expect.poll(() => createRequests).toBe(0);
	page.off('request', countCreateRequests);

	await dialog.getByLabel('Username', { exact: true }).fill(`  ${username}  `);
	await dialog.getByLabel('Password *', { exact: true }).fill('  abcdefgh  ');
	const policyResponsePromise = page.waitForResponse(
		(response) =>
			response.status() === 400 &&
			response.request().method() === 'POST' &&
			new URL(response.url()).pathname === '/api/users'
	);
	await dialog.getByRole('button', { name: 'Create User', exact: true }).click();
	const policyResponse = await policyResponsePromise;
	expect(policyResponse.request().postDataJSON()).toMatchObject({
		username,
		password: '  abcdefgh  '
	});
	expect(await policyResponse.json()).toMatchObject({
		status: 400,
		type: 'urn:arcane:problem:password-policy:strong'
	});
	await expect(
		page.getByText('12+ chars with upper, lower, number, and symbol.', { exact: true })
	).toBeVisible();

	await dialog.getByLabel('Password *', { exact: true }).fill(password);

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
	expect(user.username).toBe(username);

	await page.getByRole('button', { name: 'Create User', exact: true }).click();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel('Username', { exact: true })).toHaveValue('');
	await expect(dialog.getByLabel('Password *', { exact: true })).toHaveValue('');
	await expect(dialog.getByLabel('Display Name', { exact: true })).toHaveValue('');
	await expect(dialog.getByLabel('Email', { exact: true })).toHaveValue('');
	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
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
	await dialog.getByLabel('Password', { exact: true }).fill('short');

	let profileUpdateRequests = 0;
	const countProfileUpdateRequests = (request: Request) => {
		if (request.method() === 'PUT' && new URL(request.url()).pathname === `/api/users/${user.id}`) {
			profileUpdateRequests += 1;
		}
	};
	page.on('request', countProfileUpdateRequests);
	await dialog.getByRole('button', { name: 'Save Changes', exact: true }).click();
	await expect(
		dialog.getByText('Password must be at least 8 characters', { exact: true })
	).toBeVisible();
	await expect.poll(() => profileUpdateRequests).toBe(0);
	page.off('request', countProfileUpdateRequests);

	await dialog.getByLabel('Display Name', { exact: true }).fill('Discarded Browser Edit');
	await dialog.getByLabel('Password', { exact: true }).fill('abcdefgh');
	const policyResponsePromise = page.waitForResponse(
		(response) =>
			response.status() === 400 &&
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname === `/api/users/${user.id}`
	);
	await dialog.getByRole('button', { name: 'Save Changes', exact: true }).click();
	const policyResponse = await policyResponsePromise;
	expect(await policyResponse.json()).toMatchObject({
		status: 400,
		type: 'urn:arcane:problem:password-policy:strong'
	});
	await expect(
		page.getByText('12+ chars with upper, lower, number, and symbol.', { exact: true })
	).toBeVisible();

	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	await expect(dialog).toBeHidden();

	const reopenedMenu = await openRowActionsMenu(page, row);
	await reopenedMenu.getByRole('menuitem', { name: 'Edit', exact: true }).click();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel('Password', { exact: true })).toHaveValue('');
	await expect(dialog.getByLabel('Display Name', { exact: true })).toHaveValue(
		'Scoped Browser User'
	);
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

	const updateResponse = await updateResponsePromise;
	expect(updateResponse.request().postDataJSON()).not.toHaveProperty('password');
	const updated = await readApiData<TestUser>(updateResponse, `Update user ${user.username}`);
	await readApiData<unknown[]>(
		await assignmentsResponsePromise,
		`Retain roles for ${user.username}`
	);
	await expect(dialog).toBeHidden();
	expect(updated.displayName).toBe('Scoped Browser User Updated');
}

async function loginAs(
	page: Page,
	username: string,
	password: string,
	expectedPath: string | RegExp
) {
	await page.goto('/login');
	await page.getByLabel('Username').fill(username);
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
	await expect(page).toHaveURL(expectedPath, { timeout: 15_000 });
}

type TestContainer = { id: string; names: string[]; labels?: Record<string, string> };

/** Records forbidden API responses so pages can prove they issue no unauthorized optional requests. */
function collectForbiddenResponses(page: Page) {
	const forbidden: string[] = [];
	const record = (response: Response) => {
		if (response.status() === 403) {
			forbidden.push(`${response.request().method()} ${new URL(response.url()).pathname}`);
		}
	};
	page.on('response', record);
	return { forbidden, stop: () => page.off('response', record) };
}

async function createEnvironmentAdminViaApi(page: Page, username: string, password: string) {
	const user = await readApiData<TestUser>(
		await page.request.post('/api/users', {
			data: { username, password, displayName: 'Environment Admin Browser User' }
		}),
		`Create user ${username}`
	);
	await readApiData<unknown[]>(
		await page.request.put(`/api/users/${user.id}/role-assignments`, {
			data: { assignments: [{ roleId: 'role_admin', environmentId: '0' }] }
		}),
		`Assign the Admin role on the local environment to ${username}`
	);
	return user;
}

async function toggleAutoUpdateThroughUI(
	page: Page,
	containerId: string,
	expectEnabledAfter: boolean
) {
	const card = page
		.locator('div')
		.filter({ hasText: /^Auto-Update/ })
		.filter({ has: page.getByRole('switch') })
		.last();
	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PUT' &&
			new URL(response.url()).pathname ===
				`/api/environments/0/containers/${containerId}/auto-update`
	);
	await card.getByRole('switch').click();
	await readApiData<{ message: string }>(await responsePromise, 'Toggle container auto-update');
	await expect(
		card.getByText(expectEnabledAfter ? 'Enabled' : 'Disabled', { exact: true })
	).toBeVisible();
	const details = await readApiData<{ autoUpdateEnabled?: boolean }>(
		await page.request.get(`/api/environments/0/containers/${containerId}`),
		'Read container details'
	);
	expect(details.autoUpdateEnabled).toBe(expectEnabledAfter);
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
	const environmentAdminUsername = `e2e-env-admin-${suffix}`;
	const environmentAdminVolumeName = `e2e-env-admin-volume-${suffix}`;
	const password = 'E2e-RBAC-user-123!';

	let localRole: TestRole | null = null;
	let remoteRole: TestRole | null = null;
	let clonedRole: TestRole | null = null;
	let remoteEnvironment: TestEnvironment | null = null;
	let restrictedUser: TestUser | null = null;
	let noAccessUser: TestUser | null = null;
	let environmentAdminUser: TestUser | null = null;
	let environmentAdminVolumeCreated = false;
	let restrictedContext: BrowserContext | null = null;
	let noAccessContext: BrowserContext | null = null;
	let environmentAdminContext: BrowserContext | null = null;

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
		const duplicateUserResponse = await page.request.post('/api/users', {
			data: {
				username: `  ${username}  `,
				password,
				displayName: 'Duplicate Browser User'
			}
		});
		expect(duplicateUserResponse.status()).toBe(409);
		expect(await duplicateUserResponse.json()).toMatchObject({
			status: 409,
			detail: 'username already in use'
		});
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

		// An Admin role granted only on the local environment must reach every
		// environment-scoped page without the global settings, templates,
		// variables, or S3 destination requests those pages used to make.
		environmentAdminUser = await createEnvironmentAdminViaApi(
			page,
			environmentAdminUsername,
			password
		);
		const [targetContainer] = (
			await readApiData<TestContainer[]>(
				await page.request.get('/api/environments/0/containers?start=0&limit=50'),
				'List local containers'
			)
		).filter((container) => !container.labels?.['com.getarcaneapp.arcane.updater']);
		expect(targetContainer, 'a container without an updater label is required').toBeTruthy();
		await readApiData<unknown>(
			await page.request.post('/api/environments/0/volumes', {
				data: { name: environmentAdminVolumeName, driver: 'local' }
			}),
			`Create volume ${environmentAdminVolumeName}`
		);
		environmentAdminVolumeCreated = true;

		environmentAdminContext = await browser.newContext({
			baseURL,
			storageState: { cookies: [], origins: [] }
		});
		const environmentAdminPage = await environmentAdminContext.newPage();
		environmentAdminPage.setDefaultTimeout(10_000);
		const environmentAdminErrors = installPageErrorCollector(environmentAdminPage);
		const forbiddenResponses = collectForbiddenResponses(environmentAdminPage);
		try {
			await loginAs(environmentAdminPage, environmentAdminUsername, password, /^(?!.*\/login).*$/);

			const environmentAdmin = await readApiData<TestUser>(
				await environmentAdminPage.request.get('/api/auth/me'),
				'Get environment admin current user'
			);
			expect(environmentAdmin.permissionsByEnv['0']).toEqual(
				expect.arrayContaining([
					'containers:autoupdate',
					'images:list',
					'projects:create',
					'volumes:backup'
				])
			);
			expect(environmentAdmin.permissionsByEnv.global ?? []).toEqual([]);

			for (const path of [
				'/api/environments/0/settings',
				'/api/templates/all',
				'/api/templates/default',
				'/api/variables',
				'/api/backups/s3/options',
				`/api/environments/${remoteID}/containers`
			]) {
				const rejected = await environmentAdminPage.request.get(path);
				expect(rejected.status(), `${path} must stay forbidden`).toBe(403);
			}
			forbiddenResponses.forbidden.length = 0;

			await environmentAdminPage.goto(`/containers/${targetContainer.id}`);
			await expect(
				environmentAdminPage.getByRole('tab', { name: 'Overview', exact: true })
			).toBeVisible();
			await expect(environmentAdminPage.getByText('Auto-Update', { exact: true })).toBeVisible();
			const initialDetails = await readApiData<{ autoUpdateEnabled?: boolean }>(
				await environmentAdminPage.request.get(
					`/api/environments/0/containers/${targetContainer.id}`
				),
				'Read initial container details'
			);
			expect(typeof initialDetails.autoUpdateEnabled).toBe('boolean');
			await toggleAutoUpdateThroughUI(
				environmentAdminPage,
				targetContainer.id,
				!initialDetails.autoUpdateEnabled
			);
			await toggleAutoUpdateThroughUI(
				environmentAdminPage,
				targetContainer.id,
				initialDetails.autoUpdateEnabled === true
			);

			await environmentAdminPage.goto('/images');
			await expect(
				environmentAdminPage.getByRole('heading', { name: 'Images', exact: true })
			).toBeVisible();
			const imageListResponse = await environmentAdminPage.request.get(
				'/api/environments/0/images?start=0&limit=1'
			);
			expect(imageListResponse.ok()).toBe(true);
			const imageListBody = await imageListResponse.json();
			expect(Array.isArray(imageListBody.data)).toBe(true);
			expect(typeof imageListBody.maxImageUploadSize).toBe('number');

			await environmentAdminPage.goto('/updates');
			await expect(
				environmentAdminPage.getByRole('heading', { name: 'Updates', exact: true })
			).toBeVisible();

			const optionalRequests: string[] = [];
			const recordOptionalRequest = (request: Request) => {
				const pathname = new URL(request.url()).pathname;
				if (pathname.startsWith('/api/templates') || pathname.startsWith('/api/variables')) {
					optionalRequests.push(`${request.method()} ${pathname}`);
				}
			};
			environmentAdminPage.on('request', recordOptionalRequest);
			await environmentAdminPage.goto('/projects/new?templateId=missing-template');
			await expect(
				environmentAdminPage.getByText('Docker Compose File', { exact: true })
			).toBeVisible();
			await expect(
				environmentAdminPage.getByText(
					/cannot be loaded because you lack permission to read templates/
				)
			).toBeVisible();
			// Without default templates the editor starts empty, which hides the
			// create button until valid compose content is entered.
			const composeEditor = environmentAdminPage
				.locator('.cm-editor')
				.filter({ visible: true })
				.first();
			await setCodeMirrorValue(composeEditor, TEST_COMPOSE_YAML);
			await expect(environmentAdminPage.locator('button[data-action="create"]')).toBeVisible();
			environmentAdminPage.off('request', recordOptionalRequest);
			expect(optionalRequests).toEqual([]);

			await environmentAdminPage.goto(
				`/volumes/${encodeURIComponent(environmentAdminVolumeName)}?tab=backups`
			);
			await expect(
				environmentAdminPage.getByRole('button', { name: 'Add schedule', exact: true })
			).toBeVisible();
			await expect(
				environmentAdminPage.getByRole('button', { name: 'Create Backup', exact: true })
			).toBeVisible();
			await environmentAdminPage.getByRole('button', { name: 'Add schedule', exact: true }).click();
			const policyDialog = environmentAdminPage.getByRole('dialog');
			await expect(policyDialog).toBeVisible();
			await expect(policyDialog.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
			await policyDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
			await expect(policyDialog).toBeHidden();

			expect(
				forbiddenResponses.forbidden,
				'environment admin pages must not issue forbidden requests'
			).toEqual([]);
		} finally {
			forbiddenResponses.stop();
			environmentAdminErrors.stop();
			expect(
				environmentAdminErrors.errors,
				`Environment admin page errors:\n${formatPageErrors(environmentAdminErrors.errors)}`
			).toEqual([]);
		}

		await environmentAdminContext.close();
		environmentAdminContext = null;
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
		await environmentAdminContext?.close();

		if (restrictedUser) {
			await removeApiResource(page, `/api/users/${restrictedUser.id}`);
		}
		if (noAccessUser) {
			await removeApiResource(page, `/api/users/${noAccessUser.id}`);
		}
		if (environmentAdminUser) {
			await page.request.delete(`/api/users/${environmentAdminUser.id}`).catch(() => undefined);
		}
		if (environmentAdminVolumeCreated) {
			await page.request
				.delete(
					`/api/environments/0/volumes/${encodeURIComponent(environmentAdminVolumeName)}?force=true`
				)
				.catch(() => undefined);
		}
		for (const role of [clonedRole, localRole, remoteRole]) {
			if (role) {
				await removeApiResource(page, `/api/roles/${role.id}`);
			}
		}
		if (remoteEnvironment) {
			await removeApiResource(page, `/api/environments/${remoteEnvironment.id}`);
		}
	}
});
