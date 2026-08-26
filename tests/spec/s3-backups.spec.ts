import { test, expect, type Page } from '@playwright/test';

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
	await page.request.delete(`/api/backups/s3/${destinationId}`).catch(() => undefined);
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
	await page.request
		.delete(`/api/environments/0/volumes/${encodeURIComponent(volumeName)}?force=true`)
		.catch(() => undefined);
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
		throw new Error(`Failed to upload ${fileName}: ${response.status()} ${await response.text()}`);
	}
}

async function readVolumeFile(page: Page, volumeName: string, filePath: string) {
	const relativePath = filePath.replace(/^\/+/, '');
	const response = await page.request.get(
		`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/workspace/file?relativePath=${encodeURIComponent(relativePath)}`
	);
	if (!response.ok()) {
		return null;
	}
	const body = await response.json();
	const content = body?.data?.content;
	return typeof content === 'string' ? content : null;
}

async function listBackups(page: Page, volumeName: string): Promise<BackupEntry[]> {
	const response = await page.request.get(
		`/api/environments/0/volumes/${encodeURIComponent(volumeName)}/backups`
	);
	if (!response.ok()) {
		return [];
	}
	const body = await response.json();
	return (body?.data ?? []) as BackupEntry[];
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
				await page.request
					.delete(`/api/environments/0/volumes/backups/${backupId}`)
					.catch(() => undefined);
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

			await page.request.delete(`/api/environments/0/volumes/backups/${created.id}`);
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
				await page.request
					.delete(`/api/environments/0/volumes/backups/${backupId}`)
					.catch(() => undefined);
			}
			await removeVolumeViaApi(page, volumeName);
			await deleteDestinationViaApi(page, destination.id);
		}
	});
});
