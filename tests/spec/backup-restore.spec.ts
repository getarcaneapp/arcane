import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '../fixtures/test.fixture';
import {
	createVolumeViaApi,
	readApiData,
	removeApiResource,
	removeVolumeViaApi,
	waitForActivity
} from '../utils/fetch.util';
import { composeFile } from '../utils/report';
import {
	createBackupViaApi,
	createDestinationViaApi,
	deleteDestinationViaApi,
	listBackups,
	pick,
	readManifest,
	requestWithTransientRetry,
	runInSource,
	seedFixture,
	waitForBackup,
	waitForCompletion,
	withoutMtime,
	type FixtureSource,
	type Manifest
} from '../utils/backup.util';

type BrowseEntry = { path: string; name: string; isDirectory: boolean };
type SnapshotRecord = {
	id: string;
	status: string;
	error?: string;
	localSnapshotId?: string;
	remoteSnapshotId?: string;
};

const PROJECTS_DIR = fileURLToPath(new URL('../setup/projects/', import.meta.url));
const SYSTEM_BACKUPS_SUPPORTED = !readFileSync(composeFile, 'utf8').includes(
	'ARCANE_DATABASE_URL=postgres'
);

const PROJECT_COMPOSE = `services:
  app:
    image: public.ecr.aws/docker/library/busybox:1.37
    command: ['sleep', '3600']
    volumes:
      - data:/data

volumes:
  data: {}
`;

const MUTATE = String.raw`
printf changed > text.txt
chmod 644 text.txt
chown 0:0 text.txt
printf changed > "glob[1]/star*?.txt"
printf junk > "glob[1]/junk"
chmod 777 dir/suid
chown 1:1 dir/sub/f.txt
rm dir/sgid
rm bin.dat
printf new > extra.txt
rm rel-link
ln -s elsewhere rel-link
`;

function mutate(source: FixtureSource) {
	runInSource(source, MUTATE);
	return readManifest(source);
}

function rootListing(manifest: Manifest) {
	return Object.entries(manifest)
		.filter(([entry]) => entry !== '.' && entry.split('/').length === 2)
		.map(([entry, { type }]) => ({ path: entry.slice(2), isDirectory: type === 'dir' }))
		.sort((a, b) => a.path.localeCompare(b.path));
}

test.describe('Volume backup and restore', () => {
	async function browse(page: Page, backupId: string, folder: string) {
		const entries = await readApiData<BrowseEntry[]>(
			await requestWithTransientRetry(() =>
				page.request.get(`/api/agents/0/volumes/backups/${backupId}/files/browse`, {
					params: { path: folder, limit: '100' }
				})
			),
			`Browse ${backupId}:${folder}`
		);
		return entries
			.map(({ path: entry, isDirectory }) => ({ path: entry, isDirectory }))
			.sort((a, b) => a.path.localeCompare(b.path));
	}

	async function restore(
		page: Page,
		volume: string,
		backupId: string,
		selection?: Record<string, unknown>
	) {
		const action = selection ? 'restorefiles' : 'restore';
		const response = await requestWithTransientRetry(() =>
			page.request.post(
				`/api/agents/0/volumes/${encodeURIComponent(volume)}/backups/${backupId}?action=${action}`,
				{ data: selection ?? {}, timeout: 180_000 }
			)
		);
		expect(response.status(), await response.text()).toBe(200);
	}

	async function removeWithBackups(page: Page, volume: string) {
		for (const backup of await listBackups(page, volume)) {
			await removeApiResource(page, `/api/agents/0/volumes/backups/${backup.id}`);
		}
		await removeVolumeViaApi(page, volume);
	}

	test(
		'restores selected files and folders, then the whole volume, from a local backup',
		{ timeout: 300_000 },
		async ({ page }) => {
			const volume = `e2e-restore-local-${Date.now()}`;
			await createVolumeViaApi(page, volume);
			try {
				seedFixture(volume);
				const original = readManifest(volume);
				const created = await createBackupViaApi(page, volume, { destination: 'local' });
				const backup = await waitForBackup(page, volume, created.id);
				expect(backup.localSnapshotId).toBeTruthy();

				expect(await browse(page, backup.id, '')).toEqual(rootListing(original));
				expect(await browse(page, backup.id, 'glob[1]')).toEqual([
					{ path: 'glob[1]/star*?.txt', isDirectory: false }
				]);

				const mutated = mutate(volume);
				expect(mutated).not.toEqual(original);

				const selected = ['./text.txt', './glob[1]', './dir'];
				await restore(page, volume, backup.id, {
					paths: ['text.txt', 'glob[1]', 'dir'],
					selectAll: false
				});
				const afterSelection = readManifest(volume);
				expect(withoutMtime(pick(afterSelection, selected), ['./glob[1]', './dir'])).toEqual(
					withoutMtime(pick(original, selected), ['./glob[1]', './dir'])
				);
				const untouched = ['./bin.dat', './extra.txt', './rel-link'];
				expect(pick(afterSelection, untouched)).toEqual(pick(mutated, untouched));

				await restore(page, volume, backup.id);
				expect(withoutMtime(readManifest(volume), ['.'])).toEqual(withoutMtime(original, ['.']));
			} finally {
				await removeWithBackups(page, volume);
			}
		}
	);

	test(
		'downloads a backup and restores the archive into another volume',
		{ timeout: 300_000 },
		async ({ page }) => {
			const volume = `e2e-restore-archive-${Date.now()}`;
			const copy = `${volume}-copy`;
			await createVolumeViaApi(page, volume);
			await createVolumeViaApi(page, copy);
			try {
				seedFixture(volume);
				const original = readManifest(volume);
				const created = await createBackupViaApi(page, volume, { destination: 'local' });
				const backup = await waitForBackup(page, volume, created.id);

				const download = await page.request.get(
					`/api/agents/0/volumes/backups/${backup.id}/download`,
					{ timeout: 180_000 }
				);
				expect(download.status()).toBe(200);
				const archive = await download.body();

				const session = await readApiData<{ id: string; chunkSize: number; totalChunks: number }>(
					await page.request.post('/api/agents/0/uploads/volume-backup', {
						data: { filename: `${backup.id}.tar.gz`, size: archive.length, target: copy }
					}),
					'Create upload session'
				);
				for (let index = 0; index < session.totalChunks; index++) {
					const uploaded = await page.request.put(
						`/api/agents/0/uploads/volume-backup/${session.id}/chunks/${index}`,
						{
							data: archive.subarray(index * session.chunkSize, (index + 1) * session.chunkSize),
							headers: { 'Content-Type': 'application/octet-stream' }
						}
					);
					expect(uploaded.status(), await uploaded.text()).toBe(200);
				}
				const restored = await page.request.post(
					`/api/agents/0/volumes/${encodeURIComponent(copy)}/backups/upload`,
					{ data: { uploadId: session.id }, timeout: 180_000 }
				);
				expect(restored.status(), await restored.text()).toBe(200);

				expect(withoutRoot(readManifest(copy))).toEqual(withoutRoot(original));
			} finally {
				await removeWithBackups(page, volume);
				await removeWithBackups(page, copy);
			}
		}
	);

	test(
		'restores search matches, then the whole volume, from an S3 backup',
		{ timeout: 300_000 },
		async ({ page }) => {
			const volume = `e2e-restore-s3-${Date.now()}`;
			const destination = await createDestinationViaApi(page);
			await createVolumeViaApi(page, volume);
			try {
				seedFixture(volume);
				const original = readManifest(volume);
				const created = await createBackupViaApi(page, volume, {
					destination: 's3',
					s3DestinationId: destination.id
				});
				const backup = await waitForBackup(page, volume, created.id);
				expect(backup.destination).toBe('s3');
				expect(backup.format).toBe('rustic');
				expect(backup.remoteSnapshotId).toBeTruthy();
				expect(backup.localSnapshotId ?? '').toBe('');
				const files = await requestWithTransientRetry(() =>
					page.request.get(`/api/agents/0/volumes/backups/${backup.id}/files`)
				);
				expect(files.status(), await files.text()).toBe(200);
				expect(JSON.stringify((await files.json())?.data ?? [])).toContain('text.txt');

				const mutated = mutate(volume);
				await restore(page, volume, backup.id, { selectAll: true, search: 'star' });
				const afterSearch = readManifest(volume);
				expect(afterSearch['./glob[1]/star*?.txt']).toEqual(original['./glob[1]/star*?.txt']);
				const untouched = ['./text.txt', './extra.txt', './bin.dat'];
				expect(pick(afterSearch, untouched)).toEqual(pick(mutated, untouched));

				await restore(page, volume, backup.id);
				expect(withoutMtime(readManifest(volume), ['.'])).toEqual(withoutMtime(original, ['.']));
			} finally {
				await removeWithBackups(page, volume);
				await deleteDestinationViaApi(page, destination.id);
			}
		}
	);

	test(
		'keeps local and S3 copies of a local+S3 backup and restores from them',
		{ timeout: 300_000 },
		async ({ page }) => {
			const volume = `e2e-restore-both-${Date.now()}`;
			const destination = await createDestinationViaApi(page);
			await createVolumeViaApi(page, volume);
			try {
				seedFixture(volume);
				const original = readManifest(volume);
				const created = await createBackupViaApi(page, volume, {
					destination: 'local_s3',
					s3DestinationId: destination.id
				});
				const backup = await waitForBackup(page, volume, created.id);
				expect(backup.localSnapshotId).toBeTruthy();
				expect(backup.remoteSnapshotId).toBeTruthy();

				mutate(volume);
				await restore(page, volume, backup.id);
				expect(withoutMtime(readManifest(volume), ['.'])).toEqual(withoutMtime(original, ['.']));
			} finally {
				await removeWithBackups(page, volume);
				await deleteDestinationViaApi(page, destination.id);
			}
		}
	);
});

function withoutRoot(manifest: Manifest) {
	return Object.fromEntries(Object.entries(manifest).filter(([entry]) => entry !== '.'));
}

test.describe('Project backup and restore', () => {
	type CreatedProject = { id: string; path: string };

	async function createProject(page: Page, name: string): Promise<CreatedProject> {
		const project = await readApiData<CreatedProject>(
			await page.request.post('/api/agents/0/projects', {
				multipart: {
					project: JSON.stringify({ name, composeContent: PROJECT_COMPOSE }),
					manifest: JSON.stringify({ fileChanges: [] })
				}
			}),
			`Create project ${name}`
		);
		const deploy = await page.request.post(
			`/api/agents/0/projects/${project.id}/deploy?action=up`,
			{ timeout: 180_000 }
		);
		const lines = (await deploy.text()).trim().split('\n');
		expect(lines.at(-1), lines.join('\n')).toContain('"done":true');
		return project;
	}

	async function dataVolume(page: Page, projectId: string) {
		const options = await readApiData<{ volumes: { key: string; name: string }[] }>(
			await page.request.get(`/api/agents/0/projects/${projectId}/backup-options`),
			`Backup options for ${projectId}`
		);
		const volume = options.volumes.find(({ key }) => key === 'data')?.name;
		expect(volume, JSON.stringify(options.volumes)).toBeTruthy();
		return volume as string;
	}

	function fixtureDir(project: CreatedProject) {
		return path.join(PROJECTS_DIR, path.basename(project.path), 'fixture');
	}

	async function backup(page: Page, projectId: string, data: Record<string, unknown>) {
		const created = await readApiData<SnapshotRecord>(
			await page.request.post(`/api/agents/0/projects/${projectId}/backups`, {
				data: { stopContainers: false, ...data },
				timeout: 180_000
			}),
			`Back up project ${projectId}`
		);
		return waitForCompletion(
			async () =>
				readApiData<SnapshotRecord>(
					await page.request.get(`/api/agents/0/projects/${projectId}/backups/${created.id}`),
					`Read project backup ${created.id}`
				),
			'project backup'
		);
	}

	async function restore(
		page: Page,
		projectId: string,
		backupId: string,
		data: Record<string, unknown>
	) {
		const started = await readApiData<{ activityId: string }>(
			await page.request.post(`/api/agents/0/projects/${projectId}/backups/${backupId}/restore`, {
				data,
				timeout: 180_000
			}),
			`Restore project backup ${backupId}`
		);
		await waitForActivity(page, started.activityId, 180_000);
	}

	async function removeProject(page: Page, project: CreatedProject) {
		runInSource(path.dirname(fixtureDir(project)), 'rm -rf fixture');
		const projectId = project.id;
		const backups = await readApiData<SnapshotRecord[]>(
			await page.request.get(`/api/agents/0/projects/${projectId}/backups`, {
				params: { limit: '100' }
			}),
			`List project backups for ${projectId}`
		);
		for (const entry of backups) {
			await removeApiResource(page, `/api/agents/0/projects/${projectId}/backups/${entry.id}`);
		}
		await removeApiResource(page, `/api/agents/0/projects/${projectId}`, {
			data: { removeVolumes: true }
		});
	}

	test(
		'restores files and volumes in place and as a new project from a local backup',
		{ timeout: 300_000 },
		async ({ page }) => {
			const name = `e2e-project-local-${Date.now()}`;
			const projects: CreatedProject[] = [];
			try {
				const project = await createProject(page, name);
				projects.push(project);
				const files = fixtureDir(project);
				const volume = await dataVolume(page, project.id);
				seedFixture(files);
				seedFixture(volume);
				const originalFiles = readManifest(files);
				const originalVolume = readManifest(volume);

				const snapshot = await backup(page, project.id, { destination: 'local' });
				expect(snapshot.localSnapshotId).toBeTruthy();

				mutate(files);
				mutate(volume);
				await restore(page, project.id, snapshot.id, { files: true, redeploy: false });
				expect(readManifest(files)).toEqual(originalFiles);
				expect(withoutMtime(readManifest(volume), ['.'])).toEqual(
					withoutMtime(originalVolume, ['.'])
				);

				const copy = await readApiData<CreatedProject>(
					await page.request.post(
						`/api/agents/0/projects/${project.id}/backups/${snapshot.id}/restore-as-new`,
						{ data: { name: `${name}-copy`, start: false }, timeout: 180_000 }
					),
					'Restore project as new'
				);
				projects.push(copy);
				expect(readManifest(fixtureDir(copy))).toEqual(originalFiles);
				expect(withoutMtime(readManifest(await dataVolume(page, copy.id)), ['.'])).toEqual(
					withoutMtime(originalVolume, ['.'])
				);
			} finally {
				for (const created of projects.reverse()) await removeProject(page, created);
			}
		}
	);

	test(
		'restores only the selected volume from an S3 backup and redeploys',
		{ timeout: 300_000 },
		async ({ page }) => {
			const name = `e2e-project-s3-${Date.now()}`;
			const destination = await createDestinationViaApi(page);
			let project: CreatedProject | undefined;
			try {
				project = await createProject(page, name);
				const files = fixtureDir(project);
				const volume = await dataVolume(page, project.id);
				seedFixture(files);
				seedFixture(volume);
				const originalVolume = readManifest(volume);

				const snapshot = await backup(page, project.id, {
					destination: 's3',
					s3DestinationId: destination.id
				});
				expect(snapshot.remoteSnapshotId).toBeTruthy();

				const mutatedFiles = mutate(files);
				mutate(volume);
				await restore(page, project.id, snapshot.id, {
					files: false,
					volumes: ['data'],
					redeploy: true
				});
				await expect
					.poll(
						async () =>
							(
								await readApiData<{ status: string }>(
									await page.request.get(`/api/agents/0/projects/${project?.id}`),
									'Read restored project'
								)
							).status,
						{ timeout: 60_000 }
					)
					.toBe('running');
				expect(withoutMtime(readManifest(volume), ['.'])).toEqual(
					withoutMtime(originalVolume, ['.'])
				);
				expect(readManifest(files)).toEqual(mutatedFiles);
			} finally {
				if (project) await removeProject(page, project);
				await deleteDestinationViaApi(page, destination.id);
			}
		}
	);
});

test.describe.skipIf(!SYSTEM_BACKUPS_SUPPORTED)('System backup and restore', () => {
	const FIXTURE = 'e2e-system-fixture';

	function dataVolume() {
		const container = execFileSync('docker', ['compose', '-f', composeFile, 'ps', '-q', 'arcane'], {
			encoding: 'utf8'
		}).trim();
		const mounts = JSON.parse(
			execFileSync('docker', ['inspect', '--format', '{{json .Mounts}}', container], {
				encoding: 'utf8'
			})
		) as { Name?: string; Destination: string }[];
		const volume = mounts.find(({ Destination }) => Destination === '/app/data')?.Name;
		expect(volume, JSON.stringify(mounts)).toBeTruthy();
		return volume as string;
	}

	function seedReadableFixture(fixture: FixtureSource) {
		seedFixture(fixture);
		runInSource(fixture, 'chmod 755 . deep');
		return readManifest(fixture);
	}

	function ownedByArcane(manifest: Manifest): Manifest {
		return Object.fromEntries(
			Object.entries(manifest).map(([entry, value]) => [
				entry,
				{
					...value,
					owner: '65532:65532',
					mode:
						value.type === 'file'
							? (Number.parseInt(value.mode, 8) & 0o1777).toString(8)
							: value.mode
				}
			])
		);
	}

	function removeFixture(volume: string) {
		runInSource(volume, `rm -rf ${FIXTURE}`);
	}

	async function ensureRecoveryKey(page: Page) {
		const policies = await page.request.get('/api/backups/policies');
		expect(policies.status(), await policies.text()).toBe(200);
		if (((await policies.json()) as { recoveryKeyStored: boolean }).recoveryKeyStored) return;
		const generated = await page.request.post('/api/backups/recovery/key?action=generate');
		expect(generated.status(), await generated.text()).toBe(200);
		const stored = await page.request.put('/api/backups/recovery/key', {
			data: { recoveryKey: ((await generated.json()) as { recoveryKey: string }).recoveryKey }
		});
		expect(stored.status(), await stored.text()).toBe(200);
	}

	async function backup(page: Page, data: Record<string, unknown>) {
		const response = await page.request.post('/api/backups', { data, timeout: 180_000 });
		expect(response.status(), await response.text()).toBe(202);
		const created = (await response.json()) as SnapshotRecord;
		return waitForCompletion(
			async () =>
				(
					await readApiData<SnapshotRecord[]>(
						await page.request.get('/api/backups', { params: { limit: '100' } }),
						'List system backups'
					)
				).find(({ id }) => id === created.id),
			'system backup'
		);
	}

	async function destinationIds(page: Page) {
		const response = await page.request
			.get('/api/backups/s3', { params: { limit: '100' }, timeout: 5_000 })
			.catch(() => undefined);
		if (!response?.ok()) return undefined;
		return ((await response.json()) as { data: { id: string }[] }).data.map(({ id }) => id);
	}

	test(
		'backs up the system to local and S3, and uploads a local backup to S3',
		{ timeout: 300_000 },
		async ({ page }) => {
			const destination = await createDestinationViaApi(page);
			const backups: string[] = [];
			try {
				await ensureRecoveryKey(page);
				const local = await backup(page, { destination: 'local' });
				backups.push(local.id);
				expect(local.localSnapshotId).toBeTruthy();

				const uploaded = await page.request.post(`/api/agents/0/backups/${local.id}/upload`, {
					data: { s3DestinationId: destination.id, recoveryKey: '' },
					timeout: 180_000
				});
				expect(uploaded.status(), await uploaded.text()).toBe(200);
				const run = (await uploaded.json()) as SnapshotRecord & { destination: string };
				expect(run.remoteSnapshotId).toBeTruthy();
				expect(run.destination).toBe('local_s3');

				const remote = await backup(page, { destination: 's3', s3DestinationId: destination.id });
				backups.push(remote.id);
				expect(remote.remoteSnapshotId).toBeTruthy();
			} finally {
				for (const backupId of backups) {
					await removeApiResource(page, `/api/agents/0/backups/${backupId}`, { data: {} });
				}
				await deleteDestinationViaApi(page, destination.id);
			}
		}
	);

	for (const destination of ['local', 's3'] as const) {
		test(
			`brings back the database and data directory with a full restore from ${destination}`,
			{ timeout: 420_000 },
			async ({ page }) => {
				const volume = dataVolume();
				const fixture = { volume, subpath: FIXTURE };
				let kept: string | undefined;
				let dropped: string | undefined;
				let snapshotId: string | undefined;
				try {
					await ensureRecoveryKey(page);
					const original = seedReadableFixture(fixture);
					kept = (await createDestinationViaApi(page, { name: `e2e-kept-${Date.now()}` })).id;

					const snapshot = await backup(
						page,
						destination === 'local' ? { destination } : { destination, s3DestinationId: kept }
					);
					snapshotId = snapshot.id;
					dropped = (await createDestinationViaApi(page, { name: `e2e-dropped-${Date.now()}` })).id;
					mutate(fixture);

					const restore = await page.request.post(
						`/api/agents/0/backups/${snapshot.id}?action=restore`,
						{ data: { recoveryKey: '' }, timeout: 180_000 }
					);
					expect(restore.status(), await restore.text()).toBe(200);

					await expect
						.poll(async () => (await destinationIds(page))?.includes(dropped as string), {
							timeout: 300_000,
							intervals: [2000]
						})
						.toBe(false);
					expect(await destinationIds(page)).toContain(kept);
					expect(readManifest(fixture)).toEqual(ownedByArcane(original));
				} finally {
					removeFixture(volume);
					if (snapshotId) {
						await removeApiResource(page, `/api/agents/0/backups/${snapshotId}`, { data: {} });
					}
					if (kept) await deleteDestinationViaApi(page, kept);
					if (dropped) await deleteDestinationViaApi(page, dropped);
				}
			}
		);
	}
});
