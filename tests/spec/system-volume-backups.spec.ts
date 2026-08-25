import { expect, test, type Page } from '@playwright/test';

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
	await page.request
		.delete(`/api/environments/0/volumes/${encodeURIComponent(volumeName)}?force=true`)
		.catch(() => undefined);
}

async function mockSystemBackupPage(
	page: Page,
	collection: SystemVolumeBackupPolicyCollection,
	options: { name: string; anonymous: boolean; available: boolean }[],
	history: HistoryEntry[] = []
) {
	let savedCollection = structuredClone(collection);
	const runRequests: unknown[] = [];

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
		return route.fulfill({
			json: { matched: 2, succeeded: 1, failed: 0, skipped: 1, failures: [] }
		});
	});
	await page.route('**/api/backups/history**', (route) => {
		const type = new URL(route.request().url()).searchParams.get('type');
		const rows =
			type === 'system' || type === 'volume' ? history.filter((row) => row.type === type) : history;
		return route.fulfill({ json: paginated(rows) });
	});

	return { savedCollection: () => savedCollection, runRequests: () => runRequests };
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
		await expect(systemCard.getByText('System')).toHaveClass(/text-purple-/);
		await expect(volumeCard.getByText('Volume')).toHaveClass(/text-purple-/);
		await volumeCard.getByRole('button', { name: 'Edit Schedule' }).click();
		const dialog = page.getByRole('dialog', { name: 'Edit Schedule' });

		await expect(dialog.getByText(unavailableName)).toBeVisible();
		await expect(dialog.getByText('Unavailable')).toBeVisible();
		await expect(dialog.getByText('Anonymous')).toBeVisible();
		await dialog.getByLabel('Volume selection').click();
		await page.getByRole('option', { name: 'Blocklist' }).click();
		await expect(dialog.getByLabel('Volume selection')).toContainText('Blocklist');
		await expect(dialog.getByText('Excluded volumes')).toBeVisible();
		await dialog.locator('label').filter({ hasText: liveName }).getByRole('checkbox').click();
		await dialog.getByRole('button', { name: 'Save' }).click();
		await expect(dialog).toBeHidden();
		expect(mock.savedCollection().policies[0]?.volumeNames).toEqual([unavailableName, liveName]);

		await page.getByRole('button', { name: 'Create' }).click();
		await page.getByRole('menuitem', { name: 'Schedule' }).click();
		const createSchedule = page.getByRole('dialog', { name: 'Create schedule' });
		await createSchedule.getByLabel('Backup type').click();
		await page.getByRole('option', { name: 'Volume' }).click();
		await createSchedule.getByLabel('Schedule').fill('0 30 4 * * *');
		await createSchedule.getByRole('button', { name: 'Save' }).click();
		await expect(createSchedule).toBeHidden();
		expect(mock.savedCollection().policies).toHaveLength(2);
		await expect(page.getByText('Volume', { exact: true })).toHaveCount(2);

		await page.getByRole('button', { name: 'Create' }).click();
		await page.getByRole('menuitem', { name: 'Backup' }).click();
		const createBackup = page.getByRole('dialog', { name: 'Create Backup' });
		await createBackup.getByLabel('Backup type').click();
		await page.getByRole('option', { name: 'Volume' }).click();
		await createBackup.getByLabel('Backup configuration').click();
		await page.getByRole('option', { name: '0 0 2 * * *' }).click();
		await createBackup.getByRole('button', { name: 'Create Backup' }).click();
		await expect(page.getByText('Matched 2; 1 succeeded, 0 failed, and 1 skipped.')).toBeVisible();
		expect(mock.runRequests()).toContainEqual({ policyId: 'volume-nightly' });

		await page.getByRole('button', { name: 'Create' }).click();
		await page.getByRole('menuitem', { name: 'Backup' }).click();
		const customBackup = page.getByRole('dialog', { name: 'Create Backup' });
		await customBackup.getByLabel('Backup type').click();
		await page.getByRole('option', { name: 'Volume' }).click();
		await customBackup.getByLabel('Volume selection').click();
		await page.getByRole('option', { name: 'Allowlist' }).click();
		await customBackup.locator('label').filter({ hasText: liveName }).getByRole('checkbox').click();
		await customBackup.getByRole('button', { name: 'Create Backup' }).click();
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

			await expect(page.getByRole('button', { name: 'All backups' })).toBeVisible();
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

	test('shows the same management filter and badges on a volume backup list', async ({ page }) => {
		const volumeName = `e2e-volume-history-${Date.now()}`;
		const rows = [historyEntry(volumeName, 'system'), historyEntry(volumeName, 'volume')];
		await createVolumeViaApi(page, volumeName);
		try {
			await page.route(
				`**/api/environments/0/volumes/${encodeURIComponent(volumeName)}/backups**`,
				(route) => {
					const type = new URL(route.request().url()).searchParams.get('type');
					const filtered =
						type === 'system' || type === 'volume' ? rows.filter((row) => row.type === type) : rows;
					return route.fulfill({ json: { success: true, ...paginated(filtered) } });
				}
			);

			await page.goto(`/volumes/${encodeURIComponent(volumeName)}?tab=backups`);
			const backupTable = page.getByRole('table');
			await expect(backupTable.getByText('System-managed')).toHaveClass(/text-purple-/);
			await expect(backupTable.getByText('Volume-managed')).toHaveClass(/text-purple-/);
			await page.getByTestId('facet-type-trigger').click();
			await page.getByTestId('facet-type-option-system').click();
			await expect(backupTable.getByText('System-managed')).toBeVisible();
			await expect(backupTable.getByText('Volume-managed')).toHaveCount(0);
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});
});
