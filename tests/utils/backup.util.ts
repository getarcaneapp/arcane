import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { expect, type APIResponse, type Page } from 'playwright/test';
import { readApiData, removeApiResource } from './fetch.util';

const HELPER_IMAGE = 'public.ecr.aws/docker/library/alpine:3.20';

const FIXTURE_SCRIPT = readFileSync(new URL('./backup-fixture.sh', import.meta.url), 'utf8');
const MANIFEST_SCRIPT = readFileSync(new URL('./backup-manifest.sh', import.meta.url), 'utf8');

export type ManifestEntry = {
	type: 'file' | 'dir' | 'link';
	mode: string;
	owner: string;
	size: string;
	mtime: string;
	content: string;
};

export type Manifest = Record<string, ManifestEntry>;

export type FixtureSource = string | { volume: string; subpath: string };

function mountArgs(source: FixtureSource, readOnly: boolean) {
	if (typeof source === 'string') return ['-v', `${source}:/v${readOnly ? ':ro' : ''}`];
	execFileSync('docker', [
		'run',
		'--rm',
		'-v',
		`${source.volume}:/volume`,
		HELPER_IMAGE,
		'mkdir',
		'-p',
		`/volume/${source.subpath}`
	]);
	return [
		'--mount',
		`type=volume,src=${source.volume},dst=/v,volume-subpath=${source.subpath}${readOnly ? ',readonly' : ''}`
	];
}

function runHelper(source: FixtureSource, script: string, readOnly = false) {
	return execFileSync(
		'docker',
		['run', '--rm', ...mountArgs(source, readOnly), HELPER_IMAGE, 'sh', '-c', script],
		{ encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 }
	);
}

export function seedFixture(source: FixtureSource) {
	runHelper(source, FIXTURE_SCRIPT);
}

export function runInSource(source: FixtureSource, script: string) {
	runHelper(source, `set -e\ncd /v\n${script}`);
}

export function readManifest(source: FixtureSource): Manifest {
	const manifest: Manifest = {};
	for (const line of runHelper(source, MANIFEST_SCRIPT, true).split('\n')) {
		if (!line) continue;
		const [path, type, mode, owner, size, mtime, content] = line.split('\t');
		manifest[path] = { type, mode, owner, size, mtime, content } as ManifestEntry;
	}
	return manifest;
}

export function withoutMtime(manifest: Manifest, paths: string[]): Manifest {
	return Object.fromEntries(
		Object.entries(manifest).map(([path, entry]) => [
			path,
			paths.includes(path) ? { ...entry, mtime: '-' } : entry
		])
	);
}

export function pick(manifest: Manifest, prefixes: string[]): Manifest {
	return Object.fromEntries(
		Object.entries(manifest).filter(([path]) =>
			prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`))
		)
	);
}

export const S3_ENDPOINT = process.env.E2E_S3_ENDPOINT ?? 'http://garage:3900';
export const S3_BUCKET = process.env.E2E_S3_BUCKET ?? 'arcane-backups';
export const S3_ACCESS_KEY_ID =
	process.env.E2E_S3_ACCESS_KEY_ID ?? 'GK0123456789abcdef0123456789abcdef';
export const S3_SECRET_ACCESS_KEY =
	process.env.E2E_S3_SECRET_ACCESS_KEY ??
	'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

export type S3Destination = { id: string; name: string; bucket: string; secretConfigured: boolean };

export type VolumeBackup = {
	id: string;
	status: string;
	destination: string;
	format: string;
	trigger: string;
	remoteSnapshotId?: string;
	localSnapshotId?: string;
	error?: string;
};

export function destinationPayload(overrides: Record<string, unknown> = {}) {
	return {
		name: `e2e-garage-${Date.now()}`,
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

export async function createDestinationViaApi(
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

export async function deleteDestinationViaApi(page: Page, destinationId: string) {
	await removeApiResource(page, `/api/backups/s3/${destinationId}`);
}

export async function listBackups(page: Page, volumeName: string): Promise<VolumeBackup[]> {
	const backups = await readApiData<VolumeBackup[]>(
		await page.request.get(
			`/api/agents/0/volumes/${encodeURIComponent(volumeName)}/backups?limit=100`
		),
		`List backups for ${volumeName}`
	);
	expect(Array.isArray(backups)).toBe(true);
	return backups;
}

// S3 and Rustic round trips occasionally return a transient 5xx; retry those
// while still failing fast on any 4xx so real backend bugs are not masked.
export async function requestWithTransientRetry(
	send: () => Promise<APIResponse>,
	attempts = 3
): Promise<APIResponse> {
	let response = await send();
	for (let attempt = 1; attempt < attempts && response.status() >= 500; attempt++) {
		await new Promise((resolve) => setTimeout(resolve, 1000));
		response = await send();
	}
	return response;
}

// Backups run Rustic in a helper container, so the request returns before the
// snapshot finishes. Poll the record until it leaves the running state.
export async function waitForCompletion<T extends { status: string; error?: string }>(
	read: () => Promise<T | undefined>,
	what: string
): Promise<T> {
	let last: T | undefined;
	await expect
		.poll(
			async () => {
				last = await read();
				return last?.status ?? 'missing';
			},
			{ timeout: 180_000, intervals: [1000] }
		)
		.not.toBe('running');
	expect(last?.status, `${what} failed: ${last?.error ?? 'unknown error'}`).toBe('succeeded');
	return last as T;
}

export async function waitForBackup(page: Page, volumeName: string, backupId: string) {
	return waitForCompletion(
		async () => (await listBackups(page, volumeName)).find((entry) => entry.id === backupId),
		'backup'
	);
}

export async function createBackupViaApi(
	page: Page,
	volumeName: string,
	data: Record<string, unknown>
): Promise<VolumeBackup> {
	const response = await page.request.post(
		`/api/agents/0/volumes/${encodeURIComponent(volumeName)}/backups`,
		{ data, timeout: 180_000 }
	);
	if (!response.ok()) {
		throw new Error(`Failed to create backup: ${response.status()} ${await response.text()}`);
	}
	const body = await response.json();
	return body.data as VolumeBackup;
}
