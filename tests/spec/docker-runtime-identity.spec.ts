import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const IMAGE = process.env.ARCANE_RUNTIME_TEST_IMAGE || 'arcane:playwright-tests';
const HEALTH_PATH = '/api/health';

function docker(args: string[], options?: { stdio?: 'pipe' | 'inherit' }) {
	const output = execFileSync('docker', args, {
		encoding: 'utf8',
		stdio: options?.stdio ?? 'pipe'
	});
	return typeof output === 'string' ? output.trim() : '';
}

function uniqueName(prefix: string) {
	return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function dockerRunContainer(args: string[]) {
	return docker(['run', '-d', ...args]);
}

function dockerExec(container: string, command: string) {
	return docker(['exec', container, 'sh', '-lc', command]);
}

function dockerExecAsUser(container: string, user: string, command: string) {
	return docker(['exec', '-u', user, container, 'sh', '-lc', command]);
}

function dockerStatus(container: string) {
	return docker(['inspect', '-f', '{{.State.Status}}', container]);
}

function dockerPort(container: string) {
	const mapping = docker(['port', container, '3552/tcp']);
	return mapping.split(':').at(-1)?.trim() || '';
}

function dockerFileStat(volumePath: string, filePath: string) {
	return docker([
		'run',
		'--rm',
		'-v',
		`${volumePath}:/mnt`,
		'--entrypoint',
		'sh',
		IMAGE,
		'-lc',
		`stat -c '%u:%g' '${filePath}'`
	]);
}

function cleanupContainer(name: string) {
	try {
		docker(['rm', '-f', name], { stdio: 'inherit' });
	} catch {
		// ignore cleanup failures
	}
}

function cleanupDir(dir: string) {
	try {
		// Files may be owned by root inside the container, so use Docker to remove them.
		docker(['run', '--rm', '-v', `${dir}:/mnt`, 'alpine:latest', 'rm', '-rf', '/mnt']);
	} catch {
		// ignore
	}
	try {
		fs.rmSync(dir, { recursive: true, force: true });
	} catch {
		// ignore cleanup failures
	}
}

function cleanupNetwork(name: string) {
	try {
		docker(['network', 'rm', name], { stdio: 'inherit' });
	} catch {
		// ignore cleanup failures
	}
}

async function waitForHealth(container: string) {
	const port = dockerPort(container);
	expect(port).not.toBe('');

	await expect
		.poll(
			async () => {
				if (dockerStatus(container) !== 'running') {
					return `container:${dockerStatus(container)}`;
				}

				try {
					const response = await fetch(`http://127.0.0.1:${port}${HEALTH_PATH}`);
					return response.ok ? 'UP' : `http:${response.status}`;
				} catch {
					return 'pending';
				}
			},
			{
				timeout: 120_000,
				interval: 2_000
			}
		)
		.toBe('UP');
}

async function waitForFile(container: string, filePath: string) {
	await expect
		.poll(
			() => {
				try {
					return dockerExec(container, `test -f '${filePath}' && echo present`);
				} catch {
					return 'missing';
				}
			},
			{
				timeout: 60_000,
				interval: 1_000
			}
		)
		.toBe('present');
}

function pidOneStatus(container: string) {
	return dockerExec(container, "grep -E '^(Uid|Gid|Groups):' /proc/1/status");
}

function arcaneProcessStatuses(container: string) {
	return dockerExec(
		container,
		[
			'for f in /proc/[0-9]*/status; do',
			'  name=$(awk \'/^Name:/ {print $2}\' "$f");',
			'  [ "$name" = "arcane" ] || continue;',
			'  pid=$(awk \'/^Pid:/ {print $2}\' "$f");',
			'  ppid=$(awk \'/^PPid:/ {print $2}\' "$f");',
			'  uid=$(awk \'/^Uid:/ {print $2}\' "$f");',
			'  gid=$(awk \'/^Gid:/ {print $2}\' "$f");',
			'  groups=$(awk \'/^Groups:/ {for (i = 2; i <= NF; i++) printf("%s%s", $i, (i < NF ? "," : ""));}\' "$f");',
			'  echo "$pid:$ppid:$uid:$gid:$groups";',
			'done'
		].join(' ')
	)
		.split('\n')
		.filter(Boolean);
}

function defaultRunArgs(name: string, dataDir: string) {
	return [
		'--name',
		name,
		'-p',
		'0:3552',
		'-e',
		'ENVIRONMENT=testing',
		'-e',
		'APP_URL=http://localhost:3552',
		'-e',
		'ENCRYPTION_KEY=3JDIgolks2tJ9ymm1AdqzlYMWu0DUWyt',
		'-e',
		'JWT_SECRET=your-super-secret-jwt-key-change-this-in-production',
		'-v',
		`${dataDir}:/app/data`
	];
}

test.describe.serial('Docker runtime identity', () => {
	test.setTimeout(240_000);

	test('keeps default root runtime behavior when PUID and PGID are unset', async () => {
		const containerName = uniqueName('arcane-default');
		const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-default-'));

		try {
			dockerRunContainer([
				...defaultRunArgs(containerName, dataDir),
				'-v',
				'/var/run/docker.sock:/var/run/docker.sock',
				IMAGE
			]);

			await waitForHealth(containerName);
			await waitForFile(containerName, '/app/data/arcane.db');

			const status = pidOneStatus(containerName);
			expect(status).toContain('Uid:\t0\t0\t0\t0');
			expect(status).toContain('Gid:\t0\t0\t0\t0');
		} finally {
			cleanupContainer(containerName);
			cleanupDir(dataDir);
		}
	});

	test('runs as the requested UID and GID without chowning a mounted projects directory', async () => {
		const containerName = uniqueName('arcane-puid');
		const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-puid-data-'));
		const projectsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-puid-projects-'));
		const sentinelPath = path.join(projectsDir, 'sentinel.txt');
		fs.writeFileSync(sentinelPath, 'sentinel\n');

		const baselineProjectsStat = dockerFileStat(projectsDir, '/mnt/sentinel.txt');

		try {
			dockerRunContainer([
				...defaultRunArgs(containerName, dataDir),
				'-e',
				'PUID=1001',
				'-e',
				'PGID=1001',
				'-v',
				'/var/run/docker.sock:/var/run/docker.sock',
				'-v',
				`${projectsDir}:/app/data/projects`,
				IMAGE
			]);

			await waitForHealth(containerName);
			await waitForFile(containerName, '/app/data/arcane.db');

			const dbStat = dockerExecAsUser(
				containerName,
				'1001:1001',
				"stat -c '%u:%g' /app/data/arcane.db"
			);
			expect(dbStat).toBe('1001:1001');

			const projectsStat = dockerExec(
				containerName,
				"stat -c '%u:%g' /app/data/projects/sentinel.txt"
			);
			expect(projectsStat).toBe(baselineProjectsStat);

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(processStatuses.some((status) => status.startsWith('1:0:0:0:'))).toBe(true);
			expect(processStatuses.some((status) => /^(?!1:)\d+:1:1001:1001:/.test(status))).toBe(true);
		} finally {
			cleanupContainer(containerName);
			cleanupDir(dataDir);
			cleanupDir(projectsDir);
		}
	});

	test('supports tcp docker host mode without a mounted Unix socket', async () => {
		const networkName = uniqueName('arcane-proxy-net');
		const proxyName = uniqueName('arcane-proxy');
		const containerName = uniqueName('arcane-proxy-app');
		const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-proxy-data-'));

		try {
			docker(['network', 'create', networkName], { stdio: 'inherit' });

			dockerRunContainer([
				'--name',
				proxyName,
				'--network',
				networkName,
				'-e',
				'EVENTS=1',
				'-e',
				'PING=1',
				'-e',
				'VERSION=1',
				'-e',
				'AUTH=0',
				'-e',
				'POST=1',
				'-e',
				'CONTAINERS=1',
				'-e',
				'IMAGES=1',
				'-e',
				'INFO=1',
				'-e',
				'NETWORKS=1',
				'-e',
				'VOLUMES=1',
				'-v',
				'/var/run/docker.sock:/var/run/docker.sock:ro',
				'tecnativa/docker-socket-proxy:latest'
			]);
			await new Promise((resolve) => setTimeout(resolve, 2_000));

			dockerRunContainer([
				...defaultRunArgs(containerName, dataDir),
				'--network',
				networkName,
				'-e',
				'PUID=1001',
				'-e',
				'PGID=1001',
				'-e',
				`DOCKER_HOST=tcp://${proxyName}:2375`,
				IMAGE
			]);

			await waitForHealth(containerName);
			await waitForFile(containerName, '/app/data/arcane.db');

			const dbStat = dockerExecAsUser(
				containerName,
				'1001:1001',
				"stat -c '%u:%g' /app/data/arcane.db"
			);
			expect(dbStat).toBe('1001:1001');

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(processStatuses.some((status) => status.startsWith('1:0:0:0:'))).toBe(true);
			expect(processStatuses.some((status) => /^(?!1:)\d+:1:1001:1001:/.test(status))).toBe(true);
		} finally {
			cleanupContainer(containerName);
			cleanupContainer(proxyName);
			cleanupNetwork(networkName);
			cleanupDir(dataDir);
		}
	});
});
