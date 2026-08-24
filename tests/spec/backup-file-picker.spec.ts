import { expect, test, type Page, type Route } from '@playwright/test';

const VOLUME_NAME = 'backup-picker-e2e';
const BACKUP_ID = 'backup-picker-snapshot';
const SYSTEM_BACKUP_ID = 'system-picker-id';
const PROJECT_ID = 'backup-picker-project';

type BrowseRequest = {
	path: string;
	search: string;
	start: number;
	limit: string | null;
};

type BrowseHandler = (request: BrowseRequest, route: Route) => Promise<void>;

function response(data: unknown) {
	return { success: true, data };
}

async function mockAppShell(page: Page) {
	await page.addInitScript(() => localStorage.removeItem('selectedEnvironmentId'));
	await page.route(/\/api\/auth\/me$/, async (route) => {
		await route.fulfill({
			json: response({
				id: 'backup-picker-admin',
				username: 'arcane',
				roleAssignments: [],
				permissionsByEnv: { global: ['*'] },
				isGlobalAdmin: true,
				createdAt: new Date().toISOString()
			})
		});
	});
	await page.route(/\/api\/auth\/auto-login-config$/, async (route) => {
		await route.fulfill({ json: response({ enabled: false }) });
	});
	await page.route(/\/api\/environments(?:\?.*)?$/, async (route) => {
		await route.fulfill({
			json: {
				success: true,
				data: [
					{ id: '0', name: 'Local', apiUrl: '', status: 'online', enabled: true, isEdge: false }
				],
				pagination: { currentPage: 1, totalPages: 1, totalItems: 1, itemsPerPage: 1000 }
			}
		});
	});
	await page.route(/\/api\/environments\/0\/settings$/, async (route) => {
		await route.fulfill({ json: {} });
	});
	await page.route(/\/api\/environments\/0\/swarm\/status$/, async (route) => {
		await route.fulfill({ json: response({ enabled: false }) });
	});
	await page.route(/\/api\/oidc\/status$/, async (route) => {
		await route.fulfill({
			json: response({
				envForced: false,
				envConfigured: false,
				mergeAccounts: false,
				providerName: '',
				providerLogoUrl: ''
			})
		});
	});
	await page.route(/\/api\/roles\/available-permissions$/, async (route) => {
		await route.fulfill({ status: 503, json: { message: 'not needed for global admin' } });
	});
	await page.route(/\/api\/app-version$/, async (route) => {
		await route.fulfill({
			json: { currentVersion: 'e2e', displayVersion: 'e2e', enabledFeatures: [] }
		});
	});
	await page.route(/\/api\/stream(?:\?.*)?$/, async (route) => {
		await route.fulfill({ contentType: 'application/x-json-stream', body: '' });
	});
	await page.route(/\/api\/backups\/s3(?:\?.*)?$/, async (route) => {
		await route.fulfill({ json: response([]) });
	});
	await page.route(/\/api\/backups\/s3\/options$/, async (route) => {
		await route.fulfill({ json: response([]) });
	});
}

async function mockVolumeBackupPage(page: Page, browse: BrowseHandler) {
	const encodedVolume = encodeURIComponent(VOLUME_NAME);
	await mockAppShell(page);
	await page.route(new RegExp(`/api/environments/0/volumes/${encodedVolume}$`), async (route) => {
		await route.fulfill({
			json: response({
				id: VOLUME_NAME,
				name: VOLUME_NAME,
				driver: 'local',
				mountpoint: `/var/lib/docker/volumes/${VOLUME_NAME}/_data`,
				scope: 'local',
				options: null,
				labels: {},
				createdAt: new Date().toISOString(),
				inUse: false,
				containers: [],
				size: 0
			})
		});
	});
	await page.route(
		new RegExp(`/api/environments/0/volumes/${encodedVolume}/backup-policy$`),
		async (route) => {
			await route.fulfill({ json: response({ policies: [], s3Available: false }) });
		}
	);
	await page.route(
		new RegExp(`/api/environments/0/volumes/${encodedVolume}/backups(?:\\?.*)?$`),
		async (route) => {
			await route.fulfill({
				json: {
					success: true,
					data: [
						{
							id: BACKUP_ID,
							volumeName: VOLUME_NAME,
							size: 1024,
							createdAt: new Date().toISOString(),
							status: 'succeeded',
							trigger: 'manual',
							destination: 'local',
							format: 'rustic',
							localSnapshotId: 'snapshot'
						}
					],
					pagination: { currentPage: 1, totalPages: 1, totalItems: 1, itemsPerPage: 10 }
				}
			});
		}
	);
	await page.route(
		new RegExp(`/api/environments/0/volumes/backups/${BACKUP_ID}/files/browse(?:\\?.*)?$`),
		async (route) => {
			const url = new URL(route.request().url());
			await browse(
				{
					path: url.searchParams.get('path') ?? '',
					search: url.searchParams.get('search') ?? '',
					start: Number(url.searchParams.get('start') ?? 0),
					limit: url.searchParams.get('limit')
				},
				route
			);
		}
	);

	await page.goto(`/volumes/${encodedVolume}?tab=backups`);
	await expect(page.getByText(BACKUP_ID, { exact: true }).first()).toBeVisible();
	const backupRow = page.getByRole('row').filter({ hasText: BACKUP_ID }).first();
	await backupRow.getByRole('button', { name: 'Open menu' }).click();
	await page.getByRole('menuitem', { name: 'Restore files' }).click();
	await expect(page.getByRole('dialog', { name: 'Restore files' })).toBeVisible();
}

async function mockProjectWorkspacePage(
	page: Page,
	workspaceState: () => { content: string; revision: number }
) {
	const encodedProject = encodeURIComponent(PROJECT_ID);
	await mockAppShell(page);
	await page.route(/\/api\/variables(?:\?.*)?$/, async (route) => {
		await route.fulfill({ json: response([]) });
	});
	await page.route(/\/api\/environments\/0\/projects\/tags$/, async (route) => {
		await route.fulfill({ json: response([]) });
	});
	await page.route(
		new RegExp(`/api/environments/0/projects/${encodedProject}/workspace$`),
		async (route) => {
			const state = workspaceState();
			await route.fulfill({
				json: response({
					files: [
						{
							path: `/projects/${PROJECT_ID}/workspace.txt`,
							relativePath: 'workspace.txt',
							name: 'workspace.txt',
							isDirectory: false,
							size: state.content.length,
							mode: '-rw-r--r--',
							isSymlink: false,
							editable: true
						}
					],
					fileTreeRevision: `revision-${state.revision}`,
					fileTreeTruncated: false
				})
			});
		}
	);
	await page.route(
		new RegExp(`/api/environments/0/projects/${encodedProject}/workspace/file(?:\\?.*)?$`),
		async (route) => {
			const state = workspaceState();
			await route.fulfill({
				json: response({
					path: `/projects/${PROJECT_ID}/workspace.txt`,
					relativePath: 'workspace.txt',
					name: 'workspace.txt',
					size: state.content.length,
					mimeType: 'text/plain',
					content: state.content,
					editable: true
				})
			});
		}
	);
	await page.route(
		new RegExp(`/api/environments/0/projects/${encodedProject}(?:/(compose|runtime|updates))?$`),
		async (route) => {
			const section = new URL(route.request().url()).pathname.split('/').at(-1);
			const project = {
				id: PROJECT_ID,
				name: PROJECT_ID,
				path: `/projects/${PROJECT_ID}`,
				dirName: PROJECT_ID,
				status: 'stopped',
				serviceCount: 0,
				runningCount: 0,
				isArchived: false,
				hasBuildDirective: false,
				createdAt: new Date().toISOString(),
				updatedAt: new Date().toISOString(),
				tags: [],
				services: [],
				runtimeServices: [],
				includeFiles: []
			};

			if (section === 'compose') {
				await route.fulfill({
					json: response({
						...project,
						composeContent: 'services: {}\n',
						envContent: '',
						overrideContent: '',
						composeFileName: 'compose.yaml'
					})
				});
				return;
			}

			await route.fulfill({ json: response(project) });
		}
	);
}

async function mockSystemBackupPage(
	page: Page,
	browse: (body: Record<string, unknown>, route: Route) => Promise<void>,
	restore: (body: Record<string, unknown>, route: Route) => Promise<void>,
	openPage = true
) {
	await mockAppShell(page);
	await page.route(/\/api\/backups(?:\?.*)?$/, async (route) => {
		await route.fulfill({
			json: {
				success: true,
				data: [
					{
						id: SYSTEM_BACKUP_ID,
						size: 2048,
						createdAt: new Date().toISOString(),
						status: 'succeeded',
						trigger: 'manual',
						destination: 'local',
						localSnapshotId: 'system-snapshot'
					}
				],
				pagination: { currentPage: 1, totalPages: 1, totalItems: 1, itemsPerPage: 20 }
			}
		});
	});
	await page.route(/\/api\/backups\/policies$/, async (route) => {
		await route.fulfill({ json: response({ policies: [], recoveryKeyStored: true }) });
	});
	await page.route(new RegExp(`/api/backups/${SYSTEM_BACKUP_ID}/files/browse$`), async (route) => {
		await browse(route.request().postDataJSON() as Record<string, unknown>, route);
	});
	await page.route(new RegExp(`/api/backups/${SYSTEM_BACKUP_ID}/restore-files$`), async (route) => {
		await restore(route.request().postDataJSON() as Record<string, unknown>, route);
	});
	if (!openPage) return;

	await page.goto('/settings/backups');
	await openSystemRestoreDialog(page);
}

async function openSystemRestoreDialog(page: Page) {
	await expect(page.getByText(SYSTEM_BACKUP_ID, { exact: false }).first()).toBeVisible();
	const backupRow = page.getByRole('row').filter({ hasText: SYSTEM_BACKUP_ID }).first();
	await backupRow.getByRole('button', { name: 'Open menu' }).click();
	await page.getByRole('menuitem', { name: 'Restore files' }).click();
	await expect(page.getByRole('dialog', { name: 'Restore files' })).toBeVisible();
}

async function navigateInApp(page: Page, path: string) {
	await page.evaluate((targetPath) => {
		const link = document.createElement('a');
		link.href = targetPath;
		document.body.append(link);
		link.click();
		link.remove();
	}, path);
	await page.waitForURL((url) => url.pathname === path.split('?')[0]);
}

function rootEntries(count: number) {
	return Array.from({ length: count }, (_, index) =>
		index === 0
			? { path: 'folder', name: 'folder', isDirectory: true }
			: {
					path: `file-${index.toString().padStart(5, '0')}.txt`,
					name: `file-${index.toString().padStart(5, '0')}.txt`,
					isDirectory: false
				}
	);
}

test.describe('Backup file picker', () => {
	test('refreshes clean volume workspace files when returning to the workspace tab', async ({
		page
	}) => {
		const encodedVolume = encodeURIComponent(VOLUME_NAME);
		let fileContent = 'before restore';
		let workspaceRequests = 0;
		let fileRequests = 0;
		const restoreBodies: Array<Record<string, unknown>> = [];

		await page.route(
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/workspace$`),
			async (route) => {
				workspaceRequests += 1;
				await route.fulfill({
					json: response({
						files: [
							{
								path: '/workspace.txt',
								relativePath: 'workspace.txt',
								name: 'workspace.txt',
								isDirectory: false,
								size: fileContent.length,
								mode: '-rw-r--r--',
								isSymlink: false,
								editable: true
							}
						],
						fileTreeRevision: `revision-${workspaceRequests}`,
						fileTreeTruncated: false
					})
				});
			}
		);
		await page.route(
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/workspace/file(?:\\?.*)?$`),
			async (route) => {
				fileRequests += 1;
				await route.fulfill({
					json: response({
						path: '/workspace.txt',
						relativePath: 'workspace.txt',
						name: 'workspace.txt',
						size: fileContent.length,
						mimeType: 'text/plain',
						content: fileContent,
						editable: true
					})
				});
			}
		);
		await page.route(
			new RegExp(
				`/api/environments/0/volumes/${encodedVolume}/backups/${BACKUP_ID}/restore-files$`
			),
			async (route) => {
				restoreBodies.push(route.request().postDataJSON() as Record<string, unknown>);
				fileContent = 'after restore';
				await route.fulfill({ json: response({ message: 'restored' }) });
			}
		);

		await mockVolumeBackupPage(page, async (_request, route) => {
			await route.fulfill({
				json: response({
					entries: [{ path: 'workspace.txt', name: 'workspace.txt', isDirectory: false }]
				})
			});
		});

		await page.keyboard.press('Escape');
		await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
		await page
			.locator('[data-path="workspace.txt"]')
			.getByRole('button', { name: 'workspace.txt' })
			.click();
		const editor = page.locator('.arcane-code-editor .cm-content').first();
		await expect(editor).toContainText('before restore');

		await page.getByRole('tab', { name: 'Backups', exact: true }).click();
		const backupRow = page.getByRole('row').filter({ hasText: BACKUP_ID }).first();
		await backupRow.getByRole('button', { name: 'Open menu' }).click();
		await page.getByRole('menuitem', { name: 'Restore files' }).click();
		const dialog = page.getByRole('dialog', { name: 'Restore files' });
		await dialog.locator('[data-path="workspace.txt"]').getByRole('checkbox').click();
		await dialog.getByRole('button', { name: 'Restore files' }).click();
		await expect.poll(() => restoreBodies.length).toBe(1);
		expect(restoreBodies[0]).toEqual({ paths: ['workspace.txt'], selectAll: false, search: '' });
		await page.getByRole('tab', { name: 'Workspace', exact: true }).click();

		await expect(editor).toContainText('after restore');
		await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(0);
		expect(workspaceRequests).toBeGreaterThanOrEqual(2);
		expect(fileRequests).toBeGreaterThanOrEqual(2);
	});

	test('refreshes clean project workspace files after a system restore', async ({ page }) => {
		const workspaceState = { content: 'before restore', revision: 1 };
		const restoreBodies: Array<Record<string, unknown>> = [];
		await mockProjectWorkspacePage(page, () => workspaceState);

		await page.goto(`/projects/${PROJECT_ID}?tab=compose`);
		await page
			.locator('[data-path="workspace.txt"]')
			.getByRole('button', { name: 'workspace.txt' })
			.click();
		const editor = page.locator('.arcane-code-editor .cm-content').first();
		await expect(editor).toContainText('before restore');

		await mockSystemBackupPage(
			page,
			async (_body, route) => {
				await route.fulfill({
					json: response({
						entries: [
							{
								path: `${PROJECT_ID}/workspace.txt`,
								name: 'workspace.txt',
								isDirectory: false
							}
						]
					})
				});
			},
			async (body, route) => {
				restoreBodies.push(body);
				workspaceState.content = 'after restore';
				workspaceState.revision += 1;
				await route.fulfill({ json: response({ message: 'restored' }) });
			},
			false
		);
		await navigateInApp(page, '/settings/backups');
		await openSystemRestoreDialog(page);

		const dialog = page.getByRole('dialog', { name: 'Restore files' });
		await dialog.locator(`[data-path="${PROJECT_ID}/workspace.txt"]`).getByRole('checkbox').click();
		await dialog.getByRole('button', { name: 'Restore files' }).click();
		await expect.poll(() => restoreBodies.length).toBe(1);

		await navigateInApp(page, `/projects/${PROJECT_ID}?tab=compose`);
		await page
			.locator('[data-path="workspace.txt"]')
			.getByRole('button', { name: 'workspace.txt' })
			.click();
		await expect(editor).toContainText('after restore');
		await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(0);
	});

	test('loads folders lazily, walks continuation pages, retains rows on retry, and bounds mounted rows', async ({
		page
	}) => {
		const requests: BrowseRequest[] = [];
		let continuationAttempts = 0;
		await mockVolumeBackupPage(page, async (request, route) => {
			requests.push(request);
			if (request.path === 'folder/nested') {
				await route.fulfill({
					json: response({
						entries: [
							{ path: 'folder/nested/grandchild.txt', name: 'grandchild.txt', isDirectory: false }
						]
					})
				});
				return;
			}
			if (request.path === 'folder') {
				await route.fulfill({
					json: response({
						entries: [
							{ path: 'folder/nested', name: 'nested', isDirectory: true },
							{ path: 'folder/child.txt', name: 'child.txt', isDirectory: false }
						]
					})
				});
				return;
			}
			if (request.start === 250) {
				continuationAttempts += 1;
				if (continuationAttempts === 1) {
					await route.fulfill({ status: 500, json: { message: 'interrupted' } });
					return;
				}
				await route.fulfill({
					json: response({
						entries: [{ path: 'last.txt', name: 'last.txt', isDirectory: false }]
					})
				});
				return;
			}
			await route.fulfill({ json: response({ entries: rootEntries(250), nextStart: 250 }) });
		});

		await expect.poll(() => requests.length).toBe(1);
		expect(requests[0]).toMatchObject({ path: '', search: '', start: 0, limit: null });
		const tree = page.locator('[data-backup-file-tree]');
		await expect(tree.locator('[data-path="folder"]')).toBeVisible();
		expect(await tree.locator('[data-path]').count()).toBeLessThan(40);

		await tree
			.locator('[data-path="folder"]')
			.getByRole('button', { name: 'Expand folder' })
			.click();
		await expect.poll(() => requests.filter((request) => request.path === 'folder').length).toBe(1);
		await expect(tree.locator('[data-path="folder/child.txt"]')).toBeVisible();
		await tree
			.locator('[data-path="folder/nested"]')
			.getByRole('button', { name: 'Expand nested' })
			.click();
		await expect
			.poll(() => requests.filter((request) => request.path === 'folder/nested').length)
			.toBe(1);
		await expect(tree.locator('[data-path="folder/nested/grandchild.txt"]')).toBeVisible();

		await tree.evaluate((element) => {
			element.scrollTop = element.scrollHeight;
			element.dispatchEvent(new Event('scroll'));
		});
		await expect(page.getByText('The remaining backup files could not be loaded.')).toBeVisible();
		expect(await tree.locator('[data-path]').count()).toBeLessThan(40);
		await tree.evaluate((element) => {
			element.scrollTop = 0;
			element.dispatchEvent(new Event('scroll'));
		});
		await expect(tree.locator('[data-path="folder"]')).toBeVisible();
		await tree.evaluate((element) => {
			element.scrollTop = element.scrollHeight;
			element.dispatchEvent(new Event('scroll'));
		});
		await expect(page.getByText('The remaining backup files could not be loaded.')).toBeVisible();
		await page.getByRole('button', { name: 'Retry' }).click();
		await expect.poll(() => continuationAttempts).toBe(2);
		await expect(tree.locator('[data-path="last.txt"]')).toBeVisible();
	});

	test('supports folder coverage, indeterminate state, global selection, and search-scoped selection', async ({
		page
	}) => {
		const restoreBodies: Array<Record<string, unknown>> = [];
		await page.route(
			new RegExp(`/api/environments/0/volumes/${VOLUME_NAME}/backups/${BACKUP_ID}/restore-files$`),
			async (route) => {
				restoreBodies.push(route.request().postDataJSON() as Record<string, unknown>);
				await route.fulfill({ json: response({ message: 'restored' }) });
			}
		);
		await mockVolumeBackupPage(page, async (request, route) => {
			if (request.search) {
				await route.fulfill({
					json: response({
						entries: [
							{
								path: `folder/${request.search}.txt`,
								name: `${request.search}.txt`,
								isDirectory: false
							}
						]
					})
				});
				return;
			}
			if (request.path === 'folder') {
				await route.fulfill({
					json: response({
						entries: [
							{ path: 'folder/a.txt', name: 'a.txt', isDirectory: false },
							{ path: 'folder/b.txt', name: 'b.txt', isDirectory: false }
						]
					})
				});
				return;
			}
			await route.fulfill({
				json: response({
					entries: [
						{ path: 'folder', name: 'folder', isDirectory: true },
						{ path: 'root.txt', name: 'root.txt', isDirectory: false }
					]
				})
			});
		});

		const dialog = page.getByRole('dialog', { name: 'Restore files' });
		const folder = dialog.locator('[data-path="folder"]');
		await folder.getByRole('button', { name: 'Expand folder' }).click();
		const child = dialog.locator('[data-path="folder/a.txt"]');
		await child.getByRole('checkbox').click();
		await expect(folder.getByRole('checkbox')).toHaveAttribute('data-state', 'indeterminate');
		await folder.getByRole('checkbox').click();
		await expect(child.getByRole('checkbox')).toBeDisabled();
		await expect(child.getByRole('checkbox')).toBeChecked();

		await dialog.getByRole('button', { name: 'Clear' }).click();
		await dialog.getByRole('button', { name: 'Select all' }).click();
		await expect(dialog.locator('[data-path="root.txt"]').getByRole('checkbox')).toBeDisabled();
		await dialog.getByRole('button', { name: 'Restore files' }).click();
		await expect.poll(() => restoreBodies.length).toBe(1);
		expect(restoreBodies[0]).toEqual({ paths: [], selectAll: true, search: '' });

		await mockVolumeBackupPage(page, async (request, route) => {
			if (request.search) {
				await route.fulfill({
					json: response({
						entries: [
							{
								path: `folder/${request.search}.txt`,
								name: `${request.search}.txt`,
								isDirectory: false
							}
						]
					})
				});
				return;
			}
			await route.fulfill({ json: response({ entries: rootEntries(2) }) });
		});
		const reopened = page.getByRole('dialog', { name: 'Restore files' });
		await reopened.getByPlaceholder('Search files').fill('match');
		await expect(reopened.locator('[data-path="folder/match.txt"]')).toBeVisible();
		await reopened.getByRole('button', { name: 'Select all search matches' }).click();
		await reopened.getByPlaceholder('Search files').fill('changed');
		await expect(reopened.getByRole('button', { name: 'Restore files' })).toBeDisabled();
	});

	test('keeps a synthetic 100,000-entry result bounded to the viewport', async ({ page }) => {
		await mockVolumeBackupPage(page, async (_request, route) => {
			await route.fulfill({ json: response({ entries: rootEntries(100_000) }) });
		});
		const tree = page.locator('[data-backup-file-tree]');
		await expect(tree.locator('[data-path="folder"]')).toBeVisible();
		expect(await tree.locator('[data-path]').count()).toBeLessThan(40);
		await tree.evaluate((element) => {
			element.scrollTop = element.scrollHeight / 2;
			element.dispatchEvent(new Event('scroll'));
		});
		await expect.poll(() => tree.locator('[data-path]').count()).toBeLessThan(40);
	});

	test('uses the same provider selection contract in the system backup dialog', async ({
		page
	}) => {
		const browseBodies: Array<Record<string, unknown>> = [];
		const restoreBodies: Array<Record<string, unknown>> = [];
		await mockSystemBackupPage(
			page,
			async (body, route) => {
				browseBodies.push(body);
				await route.fulfill({
					json: response({ entries: [{ path: 'project', name: 'project', isDirectory: true }] })
				});
			},
			async (body, route) => {
				restoreBodies.push(body);
				await route.fulfill({ json: response({ message: 'restored' }) });
			}
		);

		await expect.poll(() => browseBodies.length).toBe(1);
		expect(browseBodies[0]).toEqual({ recoveryKey: '', path: '' });
		const dialog = page.getByRole('dialog', { name: 'Restore files' });
		await dialog.getByRole('button', { name: 'Select all' }).click();
		await dialog.getByRole('button', { name: 'Restore files' }).click();
		await expect.poll(() => restoreBodies.length).toBe(1);
		expect(restoreBodies[0]).toEqual({ recoveryKey: '', paths: [], selectAll: true, search: '' });
	});
});
