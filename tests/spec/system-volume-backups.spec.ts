import { expect, test, type Page } from '@playwright/test';

type ManagementType = 'system' | 'volume';

type SystemVolumeBackupConfig = {
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

const defaultConfig: SystemVolumeBackupConfig = {
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
	config: SystemVolumeBackupConfig,
	options: { name: string; anonymous: boolean; available: boolean }[],
	history: HistoryEntry[] = []
) {
	let savedConfig = structuredClone(config);

	await page.route('**/api/backups/volumes/config', async (route) => {
		if (route.request().method() === 'PUT') {
			savedConfig = (await route.request().postDataJSON()) as SystemVolumeBackupConfig;
		}
		await route.fulfill({ json: savedConfig });
	});
	await page.route('**/api/backups/volumes/options', (route) => route.fulfill({ json: options }));
	await page.route('**/api/backups/volumes/run', (route) =>
		route.fulfill({
			json: { matched: 2, succeeded: 1, failed: 0, skipped: 1, failures: [] }
		})
	);
	await page.route('**/api/backups/history**', (route) => {
		const type = new URL(route.request().url()).searchParams.get('type');
		const rows =
			type === 'system' || type === 'volume' ? history.filter((row) => row.type === type) : history;
		return route.fulfill({ json: paginated(rows) });
	});

	return { savedConfig: () => savedConfig };
}

test.describe('System-managed volume backups', () => {
	test('edits live selection, retains unavailable names, and runs while disabled', async ({
		page
	}) => {
		const liveName = `e2e-live-volume-${Date.now()}`;
		const anonymousName = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
		const unavailableName = 'deleted-volume';
		const mock = await mockSystemBackupPage(
			page,
			{ ...defaultConfig, selectionMode: 'allowlist', volumeNames: [unavailableName] },
			[
				{ name: liveName, anonymous: false, available: true },
				{ name: anonymousName, anonymous: true, available: true },
				{ name: unavailableName, anonymous: false, available: false }
			]
		);

		await page.goto('/settings/backups');
		const card = page.getByRole('heading', { name: 'All volume backups' }).locator('../..');
		await card.getByRole('button', { name: 'Edit' }).click();
		const dialog = page.getByRole('dialog', { name: 'All volume backups' });

		await expect(dialog.getByText(unavailableName)).toBeVisible();
		await expect(dialog.getByText('Unavailable')).toBeVisible();
		await expect(dialog.getByText('Anonymous')).toBeVisible();
		await dialog.locator('label').filter({ hasText: liveName }).getByRole('checkbox').click();
		await dialog.getByRole('button', { name: 'Save' }).click();
		await expect(dialog).toBeHidden();
		expect(mock.savedConfig().volumeNames).toEqual([unavailableName, liveName]);

		await card.getByRole('button', { name: 'Run now' }).click();
		await expect(page.getByText('Matched 2; 1 succeeded, 0 failed, and 1 skipped.')).toBeVisible();
	});

	test('filters unified history and opens the owning volume backup tab', async ({ page }) => {
		const volumeName = `e2e-system-history-${Date.now()}`;
		await createVolumeViaApi(page, volumeName);
		try {
			await mockSystemBackupPage(
				page,
				defaultConfig,
				[],
				[historyEntry('central-volume', 'system'), historyEntry(volumeName, 'volume')]
			);
			await page.goto('/settings/backups');

			await expect(page.getByRole('button', { name: 'All backups' })).toBeVisible();
			await page.getByTestId('facet-type-trigger').click();
			await page.getByTestId('facet-type-option-system').click();
			await expect(page.getByText('central-volume')).toBeVisible();
			await expect(page.getByText(volumeName)).toHaveCount(0);

			await page.getByTestId('facet-type-option-system').click();
			await page.getByTestId('facet-type-option-volume').click();
			await expect(page.getByText(volumeName)).toBeVisible();
			await expect(page.getByText('central-volume')).toHaveCount(0);

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
			await expect(page.getByText('System-managed').first()).toBeVisible();
			await expect(page.getByText('Volume-managed').first()).toBeVisible();
			await page.getByTestId('facet-type-trigger').click();
			await page.getByTestId('facet-type-option-system').click();
			await expect(page.getByText('System-managed').first()).toBeVisible();
			await expect(page.getByText('Volume-managed')).toHaveCount(0);
		} finally {
			await removeVolumeViaApi(page, volumeName);
		}
	});
});
