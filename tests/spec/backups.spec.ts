import { expect, test, type Page, type Route } from '../fixtures/test.fixture';
import { setCodeMirrorValue } from '../utils/playwright.util';
import { removeApiResource, readApiData } from '../utils/fetch.util';
import { openRowActionsMenu } from '../utils/table-actions.util';
import { type Activity } from '../../frontend/src/lib/types/activity.type';

test.describe('Workspace restore', () => {
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

	type BackupFileEntry = {
		path: string;
		name: string;
		isDirectory: boolean;
	};

	function response(data: unknown) {
		return { success: true, data };
	}

	function browseResponse(
		data: BackupFileEntry[],
		totalItems = data.length,
		start = 0,
		limit = 20
	) {
		return {
			success: true,
			data,
			pagination: {
				totalPages: Math.max(1, Math.ceil(totalItems / limit)),
				totalItems,
				currentPage: Math.floor(start / limit) + 1,
				itemsPerPage: limit,
				grandTotalItems: totalItems
			}
		};
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
		await openVolumeRestoreFilesDialog(page);
	}

	type VolumeWorkspaceRestoreState = {
		content: string;
		restoredContent: string;
		revision: number;
		failRestore: boolean;
		workspaceRequests: number;
		fileRequests: number;
		restoreBodies: Array<Record<string, unknown>>;
		fullRestoreRequests: number;
	};

	async function mockVolumeWorkspaceRestore(page: Page, state: VolumeWorkspaceRestoreState) {
		const encodedVolume = encodeURIComponent(VOLUME_NAME);
		await page.route(
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/workspace$`),
			async (route) => {
				state.workspaceRequests += 1;
				await route.fulfill({
					json: response({
						files: [
							{
								path: '/workspace.txt',
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
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/workspace/file(?:\\?.*)?$`),
			async (route) => {
				state.fileRequests += 1;
				await route.fulfill({
					json: response({
						path: '/workspace.txt',
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
			new RegExp(
				`/api/environments/0/volumes/${encodedVolume}/backups/${BACKUP_ID}/restore-files$`
			),
			async (route) => {
				state.restoreBodies.push(route.request().postDataJSON() as Record<string, unknown>);
				if (state.failRestore) {
					await route.fulfill({ status: 500, json: { success: false, message: 'restore failed' } });
					return;
				}
				state.content = state.restoredContent;
				state.revision += 1;
				await route.fulfill({ json: response({ message: 'restored' }) });
			}
		);
		await page.route(
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/backups/${BACKUP_ID}/restore$`),
			async (route) => {
				state.fullRestoreRequests += 1;
				state.content = state.restoredContent;
				state.revision += 1;
				await route.fulfill({ json: response({ message: 'restored' }) });
			}
		);
		await page.route(
			new RegExp(`/api/environments/0/volumes/${encodedVolume}/usage$`),
			async (route) => {
				await route.fulfill({ json: response({ inUse: false, containers: [] }) });
			}
		);
	}

	async function openVolumeRestoreFilesDialog(page: Page) {
		await page.getByRole('tab', { name: 'Backups', exact: true }).click();
		await expect(page.getByText(BACKUP_ID, { exact: true }).first()).toBeVisible();
		const backupRow = page.getByRole('row').filter({ hasText: BACKUP_ID }).first();
		await backupRow.getByRole('button', { name: 'Open menu' }).click();
		await page.getByRole('menuitem', { name: 'Restore files' }).click();
		await expect(page.getByRole('dialog', { name: 'Restore files' })).toBeVisible();
	}

	async function selectVolumeRestoreFile(page: Page) {
		const dialog = page.getByRole('dialog', { name: 'Restore files' });
		await dialog.locator('[data-path="workspace.txt"]').getByRole('checkbox').click();
		return dialog;
	}

	async function openVolumeWorkspaceEditor(page: Page) {
		await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
		await page
			.locator('[data-path="workspace.txt"]')
			.getByRole('button', { name: 'workspace.txt', exact: true })
			.first()
			.click();
		const editor = page.locator('.arcane-code-editor .cm-content').first();
		await expect(editor).toBeVisible();
		return editor;
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
		browse: (request: BrowseRequest, body: Record<string, unknown>, route: Route) => Promise<void>,
		restore: (body: Record<string, unknown>, route: Route) => Promise<void>,
		openPage = true
	) {
		await mockAppShell(page);
		await page.route(/\/api\/backups\/history(?:\?.*)?$/, async (route) => {
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
							localSnapshotId: 'system-snapshot',
							type: 'system',
							resourceType: 'system',
							resourceName: 'Arcane system'
						}
					],
					pagination: { currentPage: 1, totalPages: 1, totalItems: 1, itemsPerPage: 20 }
				}
			});
		});
		await page.route(/\/api\/backups\/policies$/, async (route) => {
			await route.fulfill({ json: response({ policies: [], recoveryKeyStored: true }) });
		});
		await page.route(
			new RegExp(`/api/backups/${SYSTEM_BACKUP_ID}/files/browse(?:\\?.*)?$`),
			async (route) => {
				const url = new URL(route.request().url());
				await browse(
					{
						path: url.searchParams.get('path') ?? '',
						search: url.searchParams.get('search') ?? '',
						start: Number(url.searchParams.get('start') ?? 0),
						limit: url.searchParams.get('limit')
					},
					route.request().postDataJSON() as Record<string, unknown>,
					route
				);
			}
		);
		await page.route(
			new RegExp(`/api/backups/${SYSTEM_BACKUP_ID}/restore-files$`),
			async (route) => {
				await restore(route.request().postDataJSON() as Record<string, unknown>, route);
			}
		);
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
		test('refreshes clean volume workspace files after restore', async ({ page }) => {
			const state: VolumeWorkspaceRestoreState = {
				content: 'before restore',
				restoredContent: 'after restore',
				revision: 1,
				failRestore: false,
				workspaceRequests: 0,
				fileRequests: 0,
				restoreBodies: [],
				fullRestoreRequests: 0
			};
			await mockVolumeWorkspaceRestore(page, state);

			await mockVolumeBackupPage(page, async (_request, route) => {
				await route.fulfill({
					json: browseResponse([
						{ path: 'workspace.txt', name: 'workspace.txt', isDirectory: false }
					])
				});
			});

			await page.keyboard.press('Escape');
			const editor = await openVolumeWorkspaceEditor(page);
			await expect(editor).toContainText('before restore');

			await openVolumeRestoreFilesDialog(page);
			const dialog = await selectVolumeRestoreFile(page);
			await expect(dialog).not.toContainText('discard all unsaved text and staged file operations');
			await dialog.getByRole('button', { name: 'Restore files' }).click();
			await expect.poll(() => state.restoreBodies.length).toBe(1);
			expect(state.restoreBodies[0]).toEqual({
				paths: ['workspace.txt'],
				selectAll: false
			});
			await expect(dialog).not.toBeVisible();
			const workspaceRequestsAfterRestore = state.workspaceRequests;
			await page.getByRole('tab', { name: 'Workspace', exact: true }).click();

			await expect(editor).toContainText('after restore');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(0);
			expect(workspaceRequestsAfterRestore).toBeGreaterThanOrEqual(2);
			expect(state.workspaceRequests).toBe(workspaceRequestsAfterRestore);
			expect(state.fileRequests).toBeGreaterThanOrEqual(2);
		});

		test('warns before discarding volume workspace changes', async ({ page }) => {
			const state: VolumeWorkspaceRestoreState = {
				content: 'before restore',
				restoredContent: 'after restore',
				revision: 1,
				failRestore: false,
				workspaceRequests: 0,
				fileRequests: 0,
				restoreBodies: [],
				fullRestoreRequests: 0
			};
			await mockVolumeWorkspaceRestore(page, state);
			await mockVolumeBackupPage(page, async (_request, route) => {
				await route.fulfill({
					json: browseResponse([
						{ path: 'workspace.txt', name: 'workspace.txt', isDirectory: false }
					])
				});
			});

			await page.keyboard.press('Escape');
			const editor = await openVolumeWorkspaceEditor(page);
			await setCodeMirrorValue(editor, 'local draft');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(1);

			await openVolumeRestoreFilesDialog(page);
			let restoreDialog = await selectVolumeRestoreFile(page);
			await expect(restoreDialog).toContainText(
				'discard all unsaved text and staged file operations'
			);
			await restoreDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
			await expect(restoreDialog).not.toBeVisible();
			expect(state.restoreBodies).toHaveLength(0);

			await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
			await expect(editor).toContainText('local draft');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(1);

			state.failRestore = true;
			await openVolumeRestoreFilesDialog(page);
			restoreDialog = await selectVolumeRestoreFile(page);
			await expect(restoreDialog).toContainText(
				'discard all unsaved text and staged file operations'
			);
			await restoreDialog.getByRole('button', { name: 'Restore files' }).click();
			await expect.poll(() => state.restoreBodies.length).toBe(1);
			await restoreDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
			await expect(restoreDialog).not.toBeVisible();

			await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
			await expect(editor).toContainText('local draft');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(1);

			state.failRestore = false;
			await openVolumeRestoreFilesDialog(page);
			restoreDialog = await selectVolumeRestoreFile(page);
			await expect(restoreDialog).toContainText(
				'discard all unsaved text and staged file operations'
			);
			await restoreDialog.getByRole('button', { name: 'Restore files' }).click();
			await expect.poll(() => state.restoreBodies.length).toBe(2);
			await expect(restoreDialog).not.toBeVisible();

			await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
			await expect(editor).toContainText('after restore');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(0);
		});

		test('warns before a full volume restore discards workspace changes', async ({ page }) => {
			const state: VolumeWorkspaceRestoreState = {
				content: 'before restore',
				restoredContent: 'after restore',
				revision: 1,
				failRestore: false,
				workspaceRequests: 0,
				fileRequests: 0,
				restoreBodies: [],
				fullRestoreRequests: 0
			};
			await mockVolumeWorkspaceRestore(page, state);
			await mockVolumeBackupPage(page, async (_request, route) => {
				await route.fulfill({
					json: browseResponse([
						{ path: 'workspace.txt', name: 'workspace.txt', isDirectory: false }
					])
				});
			});

			await page.keyboard.press('Escape');
			const editor = await openVolumeWorkspaceEditor(page);
			await setCodeMirrorValue(editor, 'local draft');
			await page.getByRole('tab', { name: 'Backups', exact: true }).click();
			const backupRow = page.getByRole('row').filter({ hasText: BACKUP_ID }).first();
			await backupRow.getByRole('button', { name: 'Open menu' }).click();
			await page.getByRole('menuitem', { name: 'Restore', exact: true }).click();

			const confirmDialog = page.getByRole('dialog', { name: 'Restore Volume' });
			await expect(confirmDialog).toContainText(
				'discard all unsaved text and staged file operations'
			);
			await confirmDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
			await expect(confirmDialog).not.toBeVisible();
			expect(state.fullRestoreRequests).toBe(0);

			await page.getByRole('tab', { name: 'Workspace', exact: true }).click();
			await expect(editor).toContainText('local draft');
			await expect(page.getByRole('img', { name: 'Unsaved changes' })).toHaveCount(1);
		});

		test('refreshes clean project workspace files after a system restore', async ({ page }) => {
			const workspaceState = { content: 'before restore', revision: 1 };
			const restoreBodies: Array<Record<string, unknown>> = [];
			await mockProjectWorkspacePage(page, () => workspaceState);

			await page.goto(`/projects/${PROJECT_ID}?tab=compose`);
			await page
				.locator('[data-path="workspace.txt"]')
				.getByRole('button', { name: 'workspace.txt', exact: true })
				.first()
				.click();
			const editor = page.locator('.arcane-code-editor .cm-content').first();
			await expect(editor).toContainText('before restore');

			await mockSystemBackupPage(
				page,
				async (_request, _body, route) => {
					await route.fulfill({
						json: browseResponse([
							{
								path: `${PROJECT_ID}/workspace.txt`,
								name: 'workspace.txt',
								isDirectory: false
							}
						])
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
			await dialog
				.locator(`[data-path="${PROJECT_ID}/workspace.txt"]`)
				.getByRole('checkbox')
				.click();
			await dialog.getByRole('button', { name: 'Restore files' }).click();
			await expect.poll(() => restoreBodies.length).toBe(1);

			await navigateInApp(page, `/projects/${PROJECT_ID}?tab=compose`);
			await page
				.locator('[data-path="workspace.txt"]')
				.getByRole('button', { name: 'workspace.txt', exact: true })
				.first()
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
						json: browseResponse([
							{ path: 'folder/nested/grandchild.txt', name: 'grandchild.txt', isDirectory: false }
						])
					});
					return;
				}
				if (request.path === 'folder') {
					await route.fulfill({
						json: browseResponse([
							{ path: 'folder/nested', name: 'nested', isDirectory: true },
							{ path: 'folder/child.txt', name: 'child.txt', isDirectory: false }
						])
					});
					return;
				}
				if (request.start === 20) {
					continuationAttempts += 1;
					if (continuationAttempts === 1) {
						await route.fulfill({ status: 500, json: { message: 'interrupted' } });
						return;
					}
					await route.fulfill({
						json: browseResponse(
							[{ path: 'last.txt', name: 'last.txt', isDirectory: false }],
							21,
							20
						)
					});
					return;
				}
				await route.fulfill({ json: browseResponse(rootEntries(20), 21) });
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
			await expect
				.poll(() => requests.filter((request) => request.path === 'folder').length)
				.toBe(1);
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
				new RegExp(
					`/api/environments/0/volumes/${VOLUME_NAME}/backups/${BACKUP_ID}/restore-files$`
				),
				async (route) => {
					restoreBodies.push(route.request().postDataJSON() as Record<string, unknown>);
					await route.fulfill({ json: response({ message: 'restored' }) });
				}
			);
			await mockVolumeBackupPage(page, async (request, route) => {
				if (request.search) {
					await route.fulfill({
						json: browseResponse([
							{
								path: `folder/${request.search}.txt`,
								name: `${request.search}.txt`,
								isDirectory: false
							}
						])
					});
					return;
				}
				if (request.path === 'folder') {
					await route.fulfill({
						json: browseResponse([
							{ path: 'folder/a.txt', name: 'a.txt', isDirectory: false },
							{ path: 'folder/b.txt', name: 'b.txt', isDirectory: false }
						])
					});
					return;
				}
				await route.fulfill({
					json: browseResponse([
						{ path: 'folder', name: 'folder', isDirectory: true },
						{ path: 'root.txt', name: 'root.txt', isDirectory: false }
					])
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
						json: browseResponse([
							{
								path: `folder/${request.search}.txt`,
								name: `${request.search}.txt`,
								isDirectory: false
							}
						])
					});
					return;
				}
				await route.fulfill({ json: browseResponse(rootEntries(2)) });
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
				await route.fulfill({ json: browseResponse(rootEntries(100_000), 100_000, 0, 100_000) });
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
			const browseRequests: BrowseRequest[] = [];
			const browseBodies: Array<Record<string, unknown>> = [];
			const restoreBodies: Array<Record<string, unknown>> = [];
			await mockSystemBackupPage(
				page,
				async (request, body, route) => {
					browseRequests.push(request);
					browseBodies.push(body);
					await route.fulfill({
						json: browseResponse([{ path: 'project', name: 'project', isDirectory: true }])
					});
				},
				async (body, route) => {
					restoreBodies.push(body);
					await route.fulfill({ json: response({ message: 'restored' }) });
				}
			);

			await expect.poll(() => browseBodies.length).toBe(1);
			expect(browseBodies[0]).toEqual({ recoveryKey: '' });
			expect(browseRequests[0]).toMatchObject({ path: '', search: '', start: 0, limit: null });
			const dialog = page.getByRole('dialog', { name: 'Restore files' });
			await dialog.getByRole('button', { name: 'Select all' }).click();
			await dialog.getByRole('button', { name: 'Restore files' }).click();
			await expect.poll(() => restoreBodies.length).toBe(1);
			expect(restoreBodies[0]).toEqual({ recoveryKey: '', paths: [], selectAll: true, search: '' });
		});
	});
});

test.describe('S3 storage', () => {
	// The MinIO sidecar in the compose stack. Arcane reaches it over the compose
	// network, and so must the Rustic helper container it starts for each backup.
	const S3_ENDPOINT = process.env.E2E_S3_ENDPOINT ?? 'http://minio:9000';
	const S3_BUCKET = process.env.E2E_S3_BUCKET ?? 'arcane-backups';
	const S3_ACCESS_KEY_ID = process.env.E2E_S3_ACCESS_KEY_ID ?? 'arcane-test';
	const S3_SECRET_ACCESS_KEY = process.env.E2E_S3_SECRET_ACCESS_KEY ?? 'arcane-test-secret';

	type S3Destination = { id: string; name: string; bucket: string; secretConfigured: boolean };
	type BackupEntry = {
		id: string;
		status: string;
		destination: string;
		format: string;
		remoteSnapshotId?: string;
		localSnapshotId?: string;
		error?: string;
	};

	function destinationPayload(overrides: Record<string, unknown> = {}) {
		return {
			name: `e2e-minio-${Date.now()}`,
			endpoint: S3_ENDPOINT,
			bucket: S3_BUCKET,
			region: '',
			accessKeyId: S3_ACCESS_KEY_ID,
			secretAccessKey: S3_SECRET_ACCESS_KEY,
			prefix: `e2e/${Date.now()}`,
			useSsl: false,
			forcePathStyle: true,
			...overrides
		};
	}

	async function createDestinationViaApi(
		page: Page,
		overrides: Record<string, unknown> = {}
	): Promise<S3Destination> {
		const response = await page.request.post('/api/backups/s3', {
			data: destinationPayload(overrides)
		});
		if (!response.ok()) {
			throw new Error(
				`Failed to create S3 destination: ${response.status()} ${await response.text()}`
			);
		}
		return (await response.json()) as S3Destination;
	}

	async function deleteDestinationViaApi(page: Page, destinationId: string) {
		await removeApiResource(page, `/api/backups/s3/${destinationId}`);
	}

	async function createVolumeViaApi(page: Page, volumeName: string) {
		const response = await page.request.post('/api/environments/0/volumes', {
			data: { name: volumeName, driver: 'local' }
		});
		if (!response.ok()) {
			throw new Error(`Failed to create volume ${volumeName}: ${response.status()}`);
		}
	}

	async function removeVolumeViaApi(page: Page, volumeName: string) {
		await removeApiResource(
			page,
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}?force=true`
		);
	}

	async function getVolumeWorkspaceRevision(page: Page, volumeName: string) {
		const response = await page.request.get(
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/workspace`
		);
		if (!response.ok()) {
			throw new Error(
				`Failed to read ${volumeName} workspace: ${response.status()} ${await response.text()}`
			);
		}
		const body = await response.json();
		return body.data.fileTreeRevision as string;
	}

	async function writeVolumeFile(
		page: Page,
		volumeName: string,
		fileName: string,
		content: string,
		operation: 'create_file' | 'update_file' = 'create_file'
	) {
		const fileTreeRevision = await getVolumeWorkspaceRevision(page, volumeName);
		const response = await page.request.put(
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/workspace`,
			{
				multipart: {
					manifest: JSON.stringify({
						fileTreeRevision,
						fileChanges: [{ operation, relativePath: fileName, uploadIndex: 0 }]
					}),
					files: { name: fileName, mimeType: 'text/plain', buffer: Buffer.from(content) }
				}
			}
		);
		if (!response.ok()) {
			throw new Error(
				`Failed to upload ${fileName}: ${response.status()} ${await response.text()}`
			);
		}
	}

	async function readVolumeFile(page: Page, volumeName: string, filePath: string) {
		const relativePath = filePath.replace(/^\/+/, '');
		const response = await page.request.get(
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/workspace/file?relativePath=${encodeURIComponent(relativePath)}`
		);
		const data = await readApiData<{ content: string }>(response, `Read volume file ${filePath}`);
		expect(typeof data.content).toBe('string');
		return data.content;
	}

	async function listBackups(page: Page, volumeName: string): Promise<BackupEntry[]> {
		const backups = await readApiData<BackupEntry[]>(
			await page.request.get(
				`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/backups`
			),
			`List backups for ${volumeName}`
		);
		expect(Array.isArray(backups)).toBe(true);
		return backups;
	}

	// Backups run Rustic in a helper container, so the request returns before the
	// snapshot finishes. Poll the record until it leaves the running state.
	async function waitForBackup(page: Page, volumeName: string, backupId: string) {
		let last: BackupEntry | undefined;
		await expect
			.poll(
				async () => {
					last = (await listBackups(page, volumeName)).find((entry) => entry.id === backupId);
					return last?.status ?? 'missing';
				},
				{ timeout: 180000, intervals: [2000] }
			)
			.not.toBe('running');
		expect(last?.status, `backup failed: ${last?.error ?? 'unknown error'}`).toBe('succeeded');
		return last as BackupEntry;
	}

	// Deleting a destination also verifies that no managed environment still
	// references it, and is refused when one cannot be reached. Other specs create
	// environments, so the release path depends on what is registered right now.
	async function countRemoteEnvironments(page: Page) {
		const response = await page.request.get('/api/environments');
		if (!response.ok()) {
			return 0;
		}
		const body = await response.json();
		const environments = (body?.data ?? []) as { id: string }[];
		return environments.filter((environment) => environment.id !== '0').length;
	}

	async function createBackupViaApi(
		page: Page,
		volumeName: string,
		data: Record<string, unknown>
	): Promise<BackupEntry> {
		const response = await page.request.post(
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/backups`,
			{ data, timeout: 180000 }
		);
		if (!response.ok()) {
			throw new Error(`Failed to create backup: ${response.status()} ${await response.text()}`);
		}
		const body = await response.json();
		return body.data as BackupEntry;
	}

	test.describe('S3 Backups', () => {
		test('creates, tests, edits, and deletes a destination through settings', async ({ page }) => {
			test.setTimeout(120_000);
			const suffix = Date.now();
			const name = `e2e-minio-ui-${suffix}`;
			const updatedName = `${name}-updated`;
			let destinationId: string | undefined;

			try {
				await page.goto('/settings/backups/s3');
				await page.getByRole('button', { name: 'Add destination', exact: true }).first().click();

				let dialog = page.getByRole('dialog');
				await expect(dialog.getByRole('heading', { name: 'Add S3 destination' })).toBeVisible();
				await dialog.getByLabel('Name', { exact: true }).fill(name);
				await dialog.getByLabel('Endpoint', { exact: true }).fill(S3_ENDPOINT);
				await dialog.getByLabel('Bucket', { exact: true }).fill(S3_BUCKET);
				await dialog.getByLabel('Access key ID', { exact: true }).fill(S3_ACCESS_KEY_ID);
				await dialog.getByLabel('Secret access key', { exact: true }).fill(S3_SECRET_ACCESS_KEY);
				await dialog.getByLabel('Path prefix', { exact: true }).fill(`e2e/ui/${suffix}`);

				const sslSwitch = dialog.getByRole('switch', { name: 'Use SSL', exact: true });
				if (await sslSwitch.isChecked()) await sslSwitch.click();

				await dialog.getByRole('button', { name: 'Test Connection', exact: true }).click();
				await expect(
					page.getByText(`Connection to ${name} succeeded`, { exact: true })
				).toBeVisible();
				await expect(
					page.getByText('Connection verified. You can now save this destination.')
				).toBeVisible();
				await dialog.getByRole('button', { name: 'Add S3 destination', exact: true }).click();

				await expect(page.getByText(`Created ${name}`, { exact: true })).toBeVisible();
				let destinationRow = page.getByRole('row').filter({ hasText: name });
				await expect(destinationRow).toBeVisible();

				const listResponse = await page.request.get('/api/backups/s3?start=0&limit=100');
				expect(listResponse.status(), await listResponse.text()).toBe(200);
				const destinations = (await listResponse.json()).data as S3Destination[];
				destinationId = destinations.find((destination) => destination.name === name)?.id;
				expect(destinationId).toBeTruthy();

				let menu = await openRowActionsMenu(page, destinationRow);
				await menu.getByRole('menuitem', { name: 'Test Connection', exact: true }).click();
				await expect(
					page.getByText(`Connection to ${name} succeeded`, { exact: true }).last()
				).toBeVisible();

				menu = await openRowActionsMenu(page, destinationRow);
				await menu.getByRole('menuitem', { name: 'Edit', exact: true }).click();
				dialog = page.getByRole('dialog');
				await expect(dialog.getByRole('heading', { name: 'Edit S3 destination' })).toBeVisible();
				await dialog.getByLabel('Name', { exact: true }).fill(updatedName);
				await dialog.getByLabel('Path prefix', { exact: true }).fill(`e2e/ui/${suffix}/updated`);
				await dialog.getByRole('button', { name: 'Test Connection', exact: true }).click();
				await expect(
					page.getByText(`Connection to ${updatedName} succeeded`, { exact: true })
				).toBeVisible();
				await dialog.getByRole('button', { name: 'Save Changes', exact: true }).click();

				await expect(page.getByText(`Updated ${updatedName}`, { exact: true })).toBeVisible();
				destinationRow = page.getByRole('row').filter({ hasText: updatedName });
				await expect(destinationRow).toBeVisible();
				await expect
					.poll(() => countRemoteEnvironments(page), {
						message: 'Expected temporary remote environments to be removed before S3 deletion',
						timeout: 90_000,
						intervals: [1_000, 2_000]
					})
					.toBe(0);

				menu = await openRowActionsMenu(page, destinationRow);
				await menu.getByRole('menuitem', { name: 'Delete', exact: true }).click();
				const confirmDialog = page.getByRole('dialog', { name: `Delete ${updatedName}` });
				await expect(
					confirmDialog.getByRole('heading', { name: `Delete ${updatedName}` })
				).toBeVisible();
				const deleteResponsePromise = page.waitForResponse(
					(response) =>
						response.request().method() === 'DELETE' &&
						new URL(response.url()).pathname === `/api/backups/s3/${destinationId}`
				);
				await confirmDialog.getByRole('button', { name: 'Delete', exact: true }).click();
				const deleteResponse = await deleteResponsePromise;
				expect(deleteResponse.ok(), await deleteResponse.text()).toBe(true);

				await expect(page.getByText(`Deleted ${updatedName}`, { exact: true })).toBeVisible();
				await expect(destinationRow).toHaveCount(0);
				destinationId = undefined;
			} finally {
				if (destinationId) await deleteDestinationViaApi(page, destinationId);
			}
		});

		test('creates a destination and verifies connectivity against MinIO', async ({ page }) => {
			const destination = await createDestinationViaApi(page);
			try {
				expect(destination.bucket).toBe(S3_BUCKET);
				expect(destination.secretConfigured).toBe(true);

				// Uploads, downloads, and deletes a probe object in the bucket.
				const test = await page.request.post(`/api/backups/s3/${destination.id}/test`);
				expect(test.status(), await test.text()).toBe(200);
			} finally {
				await deleteDestinationViaApi(page, destination.id);
			}
		});

		test('refuses the stored secret once connection settings change', async ({ page }) => {
			const destination = await createDestinationViaApi(page);
			try {
				const withoutSecret = await page.request.put(`/api/backups/s3/${destination.id}`, {
					data: destinationPayload({
						name: destination.name,
						bucket: 'someone-elses-bucket',
						secretAccessKey: ''
					})
				});
				expect(withoutSecret.status()).toBe(400);
				expect(await withoutSecret.text()).toContain('re-enter the secret access key');

				// The same edit is accepted once the secret is supplied again.
				const withSecret = await page.request.put(`/api/backups/s3/${destination.id}`, {
					data: destinationPayload({ name: destination.name, prefix: `e2e/${Date.now()}-moved` })
				});
				expect(withSecret.status(), await withSecret.text()).toBe(200);
			} finally {
				await deleteDestinationViaApi(page, destination.id);
			}
		});

		test('backs a volume up to S3 and restores it from the bucket', async ({ page }) => {
			test.setTimeout(300000);

			const volumeName = `e2e-s3-backup-${Date.now()}`;
			const fileContent = `arcane-e2e-${Date.now()}`;
			const destination = await createDestinationViaApi(page);
			let backupId: string | undefined;

			try {
				await createVolumeViaApi(page, volumeName);
				await writeVolumeFile(page, volumeName, 'payload.txt', fileContent);
				expect(await readVolumeFile(page, volumeName, '/payload.txt')).toBe(fileContent);

				const created = await createBackupViaApi(page, volumeName, {
					destination: 's3',
					s3DestinationId: destination.id
				});
				backupId = created.id;
				const backup = await waitForBackup(page, volumeName, created.id);

				expect(backup.destination).toBe('s3');
				expect(backup.format).toBe('rustic');
				expect(backup.remoteSnapshotId, 'no snapshot was recorded in S3').toBeTruthy();
				expect(backup.localSnapshotId ?? '').toBe('');

				// Rustic can only list these paths by reading the repository back out
				// of the bucket, so this proves the snapshot really landed in MinIO.
				const files = await page.request.get(
					`/api/environments/0/volumes/backups/${created.id}/files`
				);
				expect(files.status(), await files.text()).toBe(200);
				expect(JSON.stringify((await files.json())?.data ?? [])).toContain('payload.txt');

				const replacementContent = 'x'.repeat(fileContent.length);
				await writeVolumeFile(page, volumeName, 'payload.txt', replacementContent, 'update_file');
				expect(await readVolumeFile(page, volumeName, '/payload.txt')).toBe(replacementContent);

				const restore = await page.request.post(
					`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/backups/${created.id}/restore`,
					{ data: {}, timeout: 180000 }
				);
				expect(restore.status(), await restore.text()).toBe(200);
				expect(await readVolumeFile(page, volumeName, '/payload.txt')).toBe(fileContent);
			} finally {
				if (backupId) {
					await removeApiResource(page, `/api/environments/0/volumes/backups/${backupId}`);
				}
				await removeVolumeViaApi(page, volumeName);
				await deleteDestinationViaApi(page, destination.id);
			}
		});

		test('blocks deleting a destination a backup still references', async ({ page }) => {
			test.setTimeout(300000);

			const volumeName = `e2e-s3-inuse-${Date.now()}`;
			const destination = await createDestinationViaApi(page);
			let backupId: string | undefined;

			try {
				await createVolumeViaApi(page, volumeName);
				await writeVolumeFile(page, volumeName, 'payload.txt', 'in-use');

				const created = await createBackupViaApi(page, volumeName, {
					destination: 's3',
					s3DestinationId: destination.id
				});
				backupId = created.id;
				await waitForBackup(page, volumeName, created.id);

				const blocked = await page.request.delete(`/api/backups/s3/${destination.id}`);
				expect(blocked.status()).toBe(409);

				const removed = await page.request.delete(
					`/api/environments/0/volumes/backups/${created.id}`
				);
				expect(removed.ok(), await removed.text()).toBe(true);
				backupId = undefined;

				const remoteEnvironments = await countRemoteEnvironments(page);
				const allowed = await page.request.delete(`/api/backups/s3/${destination.id}`);
				if (remoteEnvironments === 0) {
					expect(allowed.status(), await allowed.text()).toBe(200);
				} else {
					// A registered environment that cannot be checked keeps the
					// destination, so the local reference is no longer the blocker.
					expect([200, 409]).toContain(allowed.status());
				}
			} finally {
				if (backupId) {
					await removeApiResource(page, `/api/environments/0/volumes/backups/${backupId}`);
				}
				await removeVolumeViaApi(page, volumeName);
				await deleteDestinationViaApi(page, destination.id);
			}
		});
	});
});

test.describe('System and volume restore', () => {
	type ManagementType = 'system' | 'volume';

	type SystemVolumeBackupPolicy = {
		id: string;
		enabled: boolean;
		schedule: string;
		retentionCount: number;
		stopContainers: boolean;
		localEnabled: boolean;
		s3Enabled: boolean;
		selectionMode: 'all' | 'allowlist' | 'blocklist';
		volumeNames: string[];
		ignoreAnonymous: boolean;
	};

	type SystemVolumeBackupPolicyCollection = { policies: SystemVolumeBackupPolicy[] };

	type HistoryEntry = {
		id: string;
		size: number;
		createdAt: string;
		status: string;
		trigger: string;
		destination: string;
		format: string;
		policyId?: string;
		type: ManagementType;
		resourceType: 'system' | 'volume';
		resourceName: string;
	};

	const defaultPolicy: SystemVolumeBackupPolicy = {
		id: 'volume-nightly',
		enabled: false,
		schedule: '0 0 2 * * *',
		retentionCount: 7,
		stopContainers: false,
		localEnabled: true,
		s3Enabled: false,
		selectionMode: 'all',
		volumeNames: [],
		ignoreAnonymous: true
	};

	function paginated<T>(data: T[]) {
		return {
			data,
			pagination: {
				currentPage: 1,
				totalPages: data.length > 0 ? 1 : 0,
				totalItems: data.length,
				itemsPerPage: 20
			}
		};
	}

	function historyEntry(resourceName: string, type: ManagementType): HistoryEntry {
		return {
			id: `${type}-${resourceName}`,
			size: 1024,
			createdAt: new Date().toISOString(),
			status: 'succeeded',
			trigger: type === 'system' ? 'scheduled' : 'manual',
			destination: 'local',
			format: 'rustic',
			policyId: type === 'system' ? 'system-volume:test' : undefined,
			type,
			resourceType: 'volume',
			resourceName
		};
	}

	async function createVolumeViaApi(page: Page, volumeName: string) {
		const response = await page.request.post('/api/environments/0/volumes', {
			data: { name: volumeName, driver: 'local' }
		});
		expect(response.ok(), await response.text()).toBeTruthy();
	}

	async function removeVolumeViaApi(page: Page, volumeName: string) {
		await removeApiResource(
			page,
			`/api/environments/0/volumes/${encodeURIComponent(volumeName)}?force=true`
		);
	}

	async function mockSystemBackupPage(
		page: Page,
		collection: SystemVolumeBackupPolicyCollection,
		options: { name: string; anonymous: boolean; available: boolean }[],
		history: HistoryEntry[] = []
	) {
		let savedCollection = structuredClone(collection);
		const runRequests: unknown[] = [];
		const activities: Activity[] = [];
		await page.route('**/api/environments/0/activities?**', (route) => {
			const status = new URL(route.request().url()).searchParams.get('status');
			return route.fulfill({
				json: paginated(activities.filter((activity) => !status || activity.status === status))
			});
		});

		await page.route('**/api/backups/volumes/config', async (route) => {
			if (route.request().method() === 'PUT') {
				const input = (await route.request().postDataJSON()) as {
					policies: SystemVolumeBackupPolicy[];
				};
				savedCollection = {
					policies: input.policies.map((policy, index) => ({
						...policy,
						id: policy.id || `volume-created-${index}`
					}))
				};
			}
			await route.fulfill({ json: savedCollection });
		});
		await page.route('**/api/backups/policies', async (route) => {
			await route.fulfill({
				json: {
					policies: [
						{
							id: 'system-nightly',
							enabled: true,
							schedule: '0 0 3 * * *',
							retentionCount: 7,
							localEnabled: true,
							s3Enabled: false
						}
					],
					recoveryKeyStored: true
				}
			});
		});
		await page.route('**/api/backups/volumes/options', (route) => route.fulfill({ json: options }));
		await page.route('**/api/backups/volumes/run', async (route) => {
			runRequests.push(await route.request().postDataJSON());
			const now = new Date().toISOString();
			const activity: Activity = {
				id: `system-volume-run-${runRequests.length}`,
				environmentId: '0',
				type: 'resource_action',
				status: 'running',
				metadata: { action: 'run_system_volume_backups' },
				startedAt: now,
				createdAt: now
			};
			activities.push(activity);
			return route.fulfill({ status: 202, json: { activityId: activity.id, status: 'running' } });
		});
		await page.route('**/api/backups/history**', (route) => {
			const type = new URL(route.request().url()).searchParams.get('type');
			const rows =
				type === 'system' || type === 'volume'
					? history.filter((row) => row.type === type)
					: history;
			return route.fulfill({ json: paginated(rows) });
		});

		return {
			savedCollection: () => savedCollection,
			runRequests: () => runRequests,
			completeRun: () => {
				const activity = activities.at(-1);
				if (!activity) throw new Error('No backup run was accepted');
				activity.status = 'success';
				activity.endedAt = new Date().toISOString();
				activity.metadata = {
					...activity.metadata,
					matched: 2,
					succeeded: 1,
					failed: 0,
					skipped: 1,
					failures: []
				};
				history.push({
					...historyEntry('completed-batch-volume', 'system'),
					id: activity.id,
					trigger: 'manual'
				});
			}
		};
	}

	test.describe('System-managed volume backups', () => {
		test('creates multiple volume schedules and runs a saved schedule', async ({ page }) => {
			const liveName = `e2e-live-volume-${Date.now()}`;
			const anonymousName = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
			const unavailableName = 'deleted-volume';
			const mock = await mockSystemBackupPage(
				page,
				{
					policies: [
						{
							...defaultPolicy,
							enabled: false,
							selectionMode: 'allowlist',
							volumeNames: [unavailableName]
						}
					]
				},
				[
					{ name: liveName, anonymous: false, available: true },
					{ name: anonymousName, anonymous: true, available: true },
					{ name: unavailableName, anonymous: false, available: false }
				]
			);

			await page.goto('/settings/backups');
			const systemCard = page.getByTestId('backup-policy-system-system-nightly');
			const volumeCard = page.getByTestId('backup-policy-volume-volume-nightly');
			await expect(systemCard.getByText('System')).toHaveClass(/(^|\s)text-purple(\s|$)/);
			await expect(volumeCard.getByText('Volume')).toHaveClass(/(^|\s)text-purple(\s|$)/);
			await volumeCard.getByRole('button', { name: 'Edit Schedule' }).click();
			const dialog = page.getByRole('dialog', { name: 'Edit Schedule' });

			await expect(dialog.getByText(unavailableName)).toBeVisible();
			await expect(dialog.getByText('Unavailable', { exact: true })).toBeVisible();
			await expect(dialog.getByText('Anonymous', { exact: true })).toBeVisible();
			await dialog.getByLabel('Volume selection').click();
			await page.getByRole('option', { name: 'Blocklist' }).click();
			await expect(dialog.getByLabel('Volume selection')).toContainText('Blocklist');
			await expect(dialog.getByText('Excluded volumes')).toBeVisible();
			await dialog.locator('label').filter({ hasText: liveName }).getByRole('checkbox').click();
			await dialog.getByRole('button', { name: 'Save' }).click();
			await expect(dialog).toBeHidden();
			expect(mock.savedCollection().policies[0]?.volumeNames).toEqual([unavailableName, liveName]);

			await page.getByRole('button', { name: 'Create', exact: true }).first().click();
			await page.getByRole('menuitem', { name: 'Schedule' }).click();
			const createSchedule = page.getByRole('dialog', { name: 'Create schedule' });
			await createSchedule.getByLabel('Backup type').click();
			await page.getByRole('option', { name: 'Volume' }).click();
			await createSchedule.getByLabel('Schedule', { exact: true }).fill('0 30 4 * * *');
			await createSchedule.getByRole('button', { name: 'Save' }).click();
			await expect(createSchedule).toBeHidden();
			expect(mock.savedCollection().policies).toHaveLength(2);
			await expect(page.getByText('Volume', { exact: true })).toHaveCount(2);

			await page.getByRole('button', { name: 'Create', exact: true }).first().click();
			await page.getByRole('menuitem', { name: 'Backup' }).click();
			const createBackup = page.getByRole('dialog', { name: 'Create Backup' });
			const runningBackup = page.getByRole('alert').filter({ hasText: 'Backup in progress' });
			await createBackup.getByLabel('Backup type').click();
			await page.getByRole('option', { name: 'Volume' }).click();
			await createBackup.getByLabel('Backup configuration').click();
			await page.getByRole('option', { name: '0 0 2 * * *' }).click();
			await createBackup.getByRole('button', { name: 'Create Backup' }).click();
			await expect(createBackup).toBeHidden();
			await expect(page.getByText('Backup started', { exact: true })).toBeVisible();
			await expect(runningBackup).toBeVisible();
			await page.getByRole('button', { name: 'Create', exact: true }).first().click();
			await expect(page.getByRole('menuitem', { name: 'Backup', exact: true })).toBeDisabled();
			await page.keyboard.press('Escape');
			await page.reload();
			await expect(runningBackup).toBeVisible();
			mock.completeRun();
			await expect(runningBackup).toBeHidden({
				timeout: 15000
			});
			await expect(page.getByRole('table').getByText('completed-batch-volume')).toBeVisible();
			expect(mock.runRequests()).toContainEqual({ policyId: 'volume-nightly' });

			await page.getByRole('button', { name: 'Create', exact: true }).first().click();
			await page.getByRole('menuitem', { name: 'Backup' }).click();
			const customBackup = page.getByRole('dialog', { name: 'Create Backup' });
			await customBackup.getByLabel('Backup type').click();
			await page.getByRole('option', { name: 'Volume' }).click();
			await customBackup.getByLabel('Volume selection').click();
			await page.getByRole('option', { name: 'Allowlist' }).click();
			await customBackup
				.locator('label')
				.filter({ hasText: liveName })
				.getByRole('checkbox')
				.click();
			await customBackup.getByRole('button', { name: 'Create Backup' }).click();
			await expect(customBackup).toBeHidden();
			await expect(runningBackup).toBeVisible();
			expect(mock.runRequests()).toContainEqual({
				custom: {
					destination: 'local',
					s3DestinationId: '',
					stopContainers: false,
					selectionMode: 'allowlist',
					volumeNames: [liveName],
					ignoreAnonymous: true
				}
			});
		});

		test('filters unified history and opens the owning volume backup tab', async ({ page }) => {
			const volumeName = `e2e-system-history-${Date.now()}`;
			await createVolumeViaApi(page, volumeName);
			try {
				await mockSystemBackupPage(
					page,
					{ policies: [] },
					[],
					[historyEntry('central-volume', 'system'), historyEntry(volumeName, 'volume')]
				);
				await page.goto('/settings/backups');

				await page.getByTestId('facet-type-trigger').click();
				await page.getByTestId('facet-type-option-system').click();
				const historyTable = page.getByRole('table');
				await expect(historyTable.getByText('central-volume')).toBeVisible();
				await expect(historyTable.getByText(volumeName)).toHaveCount(0);

				await page.getByTestId('facet-type-option-system').click();
				await page.getByTestId('facet-type-option-volume').click();
				await expect(historyTable.getByText(volumeName)).toBeVisible();
				await expect(historyTable.getByText('central-volume')).toHaveCount(0);

				await page.keyboard.press('Escape');
				const row = page.getByRole('row').filter({ hasText: volumeName });
				await row.getByRole('button', { name: 'Open menu' }).click();
				await page.getByRole('menuitem', { name: 'Open volume backups' }).click();
				await expect(page).toHaveURL(
					new RegExp(`/volumes/${encodeURIComponent(volumeName)}\\?tab=backups`)
				);
			} finally {
				await removeVolumeViaApi(page, volumeName);
			}
		});
	});
});
