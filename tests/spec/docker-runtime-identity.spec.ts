import { test, expect } from '../fixtures/test.fixture';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const IMAGE = process.env.ARCANE_RUNTIME_TEST_IMAGE || 'arcane:playwright-tests';
const HELPER_IMAGE =
	process.env.ARCANE_RUNTIME_HELPER_IMAGE || 'public.ecr.aws/docker/library/alpine:3.20';
const HEALTH_PATH = '/api/health';
const SOCKET_PROXY_IMAGE = 'wollomatic/socket-proxy:1.13.1';
const SOCKET_PROXY_ARGUMENTS = [
	'-listenip=0.0.0.0',
	'-allowfrom=0.0.0.0/0',
	'-allowGET=(/v[\\d.]+)?/_ping',
	'-allowGET=(/v[\\d.]+)?/events(/.*)?',
	'-allowGET=(/v[\\d.]+)?/version',
	'-allowGET=(/v[\\d.]+)?/containers(/.*)?',
	'-allowGET=(/v[\\d.]+)?/exec(/.*)?',
	'-allowGET=(/v[\\d.]+)?/images(/.*)?',
	'-allowGET=(/v[\\d.]+)?/info(/.*)?',
	'-allowGET=(/v[\\d.]+)?/networks(/.*)?',
	'-allowGET=(/v[\\d.]+)?/volumes(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/_ping',
	'-allowHEAD=(/v[\\d.]+)?/events(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/version',
	'-allowHEAD=(/v[\\d.]+)?/containers(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/exec(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/images(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/info(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/networks(/.*)?',
	'-allowHEAD=(/v[\\d.]+)?/volumes(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/_ping',
	'-allowPOST=(/v[\\d.]+)?/events(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/version',
	'-allowPOST=(/v[\\d.]+)?/containers(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/exec(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/images(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/info(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/networks(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/volumes(/.*)?',
	'-allowPOST=(/v[\\d.]+)?/commit',
	'-allowPUT=(/v[\\d.]+)?/_ping',
	'-allowPUT=(/v[\\d.]+)?/events(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/version',
	'-allowPUT=(/v[\\d.]+)?/containers(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/exec(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/images(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/info(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/networks(/.*)?',
	'-allowPUT=(/v[\\d.]+)?/volumes(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/_ping',
	'-allowDELETE=(/v[\\d.]+)?/events(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/version',
	'-allowDELETE=(/v[\\d.]+)?/containers(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/exec(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/images(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/info(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/networks(/.*)?',
	'-allowDELETE=(/v[\\d.]+)?/volumes(/.*)?'
];

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

function shellQuote(value: string) {
	return `'${value.replaceAll("'", "'\"'\"'")}'`;
}

function dockerProbeContainer(container: string, command: string) {
	return docker([
		'run',
		'--rm',
		'--pid',
		`container:${container}`,
		'--volumes-from',
		container,
		HELPER_IMAGE,
		'sh',
		'-lc',
		command
	]);
}

function dockerProbeContainerAsUser(container: string, user: string, command: string) {
	return docker([
		'run',
		'--rm',
		'--pid',
		`container:${container}`,
		'--volumes-from',
		container,
		'-u',
		user,
		HELPER_IMAGE,
		'sh',
		'-lc',
		command
	]);
}

type ProcessStatus = {
	gid: string;
	groups: string;
	name: string;
	pid: string;
	ppid: string;
	uid: string;
};

function dockerStatus(container: string) {
	return docker(['inspect', '-f', '{{.State.Status}}', container]);
}

function dockerPort(container: string) {
	const mapping = docker(['port', container, '3552/tcp']);
	return mapping.split(':').at(-1)?.trim() || '';
}

function dockerLogs(container: string) {
	return docker(['logs', container]);
}

function dockerProxyRequest(network: string, proxy: string, path: string) {
	return docker([
		'run',
		'--rm',
		'--network',
		network,
		HELPER_IMAGE,
		'wget',
		'-qO-',
		`http://${proxy}:2375${path}`
	]);
}

function dockerProxyStatus(network: string, proxy: string, path: string) {
	const url = `http://${proxy}:2375${path}`;
	return docker([
		'run',
		'--rm',
		'--network',
		network,
		HELPER_IMAGE,
		'sh',
		'-lc',
		`wget -S -O /dev/null ${shellQuote(url)} 2>&1 | sed -n 's/.*HTTP\\/[0-9.]* \\([0-9][0-9][0-9]\\).*/\\1/p' | tail -n 1`
	]);
}

function dockerFileStat(volumePath: string, filePath: string) {
	return docker([
		'run',
		'--rm',
		'-v',
		`${volumePath}:/mnt`,
		HELPER_IMAGE,
		'sh',
		'-lc',
		`stat -c '%u:%g' ${shellQuote(filePath)}`
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
		docker(['run', '--rm', '-v', `${dir}:/mnt`, HELPER_IMAGE, 'rm', '-rf', '/mnt']);
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

function cleanupVolume(name: string) {
	try {
		docker(['volume', 'rm', '-f', name], { stdio: 'inherit' });
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
				intervals: [2_000]
			}
		)
		.toBe('UP');
}

async function waitForFile(container: string, filePath: string) {
	await expect
		.poll(
			() => {
				try {
					return dockerProbeContainer(container, `test -f ${shellQuote(filePath)} && echo present`);
				} catch {
					return 'missing';
				}
			},
			{
				timeout: 60_000,
				intervals: [1_000]
			}
		)
		.toBe('present');
}

function parseStatusBlock(status: string): ProcessStatus {
	const fields = new Map<string, string>();

	for (const line of status.split('\n')) {
		const [key, ...valueParts] = line.split(':');
		if (!key || valueParts.length === 0) continue;
		fields.set(key, valueParts.join(':').trim());
	}

	return {
		gid: fields.get('Gid') ?? '',
		groups: fields.get('Groups') ?? '',
		name: fields.get('Name') ?? '',
		pid: fields.get('Pid') ?? '',
		ppid: fields.get('PPid') ?? '',
		uid: fields.get('Uid') ?? ''
	};
}

function pidOneStatus(container: string) {
	return parseStatusBlock(dockerProbeContainer(container, 'cat /proc/1/status'));
}

function arcaneProcessStatuses(container: string) {
	const output = dockerProbeContainer(
		container,
		[
			'for f in /proc/[0-9]*/status; do',
			'  printf "%s\\n" "---ARCANE-PROCESS-STATUS---";',
			'  cat "$f";',
			'done'
		].join(' ')
	);

	return output
		.split('---ARCANE-PROCESS-STATUS---')
		.map((block) => parseStatusBlock(block))
		.filter((status) => status.name === 'arcane');
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

	test('uses the default non-root runtime when PUID and PGID are unset', async () => {
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
			expect(status.uid).toBe('0\t0\t0\t0');
			expect(status.gid).toBe('0\t0\t0\t0');

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(
				processStatuses.some(
					(status) =>
						status.pid !== '1' &&
						status.ppid === '1' &&
						status.uid.startsWith('65532\t') &&
						status.gid.startsWith('65532\t')
				)
			).toBe(true);
		} finally {
			cleanupContainer(containerName);
			cleanupDir(dataDir);
		}
	});

	test('runs as the requested UID and GID without chowning a mounted projects directory', async () => {
		const containerName = uniqueName('arcane-puid');
		const dataVolume = uniqueName('arcane-puid-data');
		const projectsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-puid-projects-'));
		const sentinelPath = path.join(projectsDir, 'sentinel.txt');
		fs.writeFileSync(sentinelPath, 'sentinel\n');
		docker(['volume', 'create', dataVolume]);

		const baselineProjectsStat = dockerFileStat(projectsDir, '/mnt/sentinel.txt');

		try {
			dockerRunContainer([
				...defaultRunArgs(containerName, dataVolume),
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

			const dbStat = dockerProbeContainerAsUser(
				containerName,
				'1001:1001',
				"stat -c '%u:%g' /app/data/arcane.db"
			);
			expect(dbStat).toBe('1001:1001');

			const projectsStat = dockerProbeContainer(
				containerName,
				"stat -c '%u:%g' /app/data/projects/sentinel.txt"
			);
			expect(projectsStat).toBe(baselineProjectsStat);

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(
				processStatuses.some(
					(status) => status.pid === '1' && status.ppid === '0' && status.uid.startsWith('0\t')
				)
			).toBe(true);
			expect(
				processStatuses.some(
					(status) =>
						status.pid !== '1' &&
						status.ppid === '1' &&
						status.uid.startsWith('1001\t') &&
						status.gid.startsWith('1001\t')
				)
			).toBe(true);

			const dockerConfigStat = dockerProbeContainerAsUser(
				containerName,
				'1001:1001',
				"stat -c '%u:%g' /app/data/.docker"
			);
			expect(dockerConfigStat).toBe('1001:1001');
			expect(dockerLogs(containerName)).not.toContain('/root/.docker/config.json');
		} finally {
			cleanupContainer(containerName);
			cleanupVolume(dataVolume);
			cleanupDir(projectsDir);
		}
	});

	test('default non-root runtime prepares mounted writable roots', async () => {
		const containerName = uniqueName('arcane-default-nonroot');
		const dataVolume = uniqueName('arcane-default-nonroot-data');
		const projectsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-default-nonroot-projects-'));
		const buildsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'arcane-default-nonroot-builds-'));
		docker(['volume', 'create', dataVolume]);

		try {
			dockerRunContainer([
				...defaultRunArgs(containerName, dataVolume),
				'-v',
				'/var/run/docker.sock:/var/run/docker.sock',
				'-v',
				`${projectsDir}:/app/data/projects`,
				'-v',
				`${buildsDir}:/builds`,
				IMAGE
			]);

			await waitForHealth(containerName);
			await waitForFile(containerName, '/app/data/arcane.db');

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(
				processStatuses.some(
					(status) =>
						status.pid !== '1' &&
						status.ppid === '1' &&
						status.uid.startsWith('65532\t') &&
						status.gid.startsWith('65532\t')
				)
			).toBe(true);

			const dataStat = dockerProbeContainerAsUser(
				containerName,
				'65532:65532',
				"stat -c '%u:%g' /app/data/arcane.db"
			);
			expect(dataStat).toBe('65532:65532');

			const projectWrite = dockerProbeContainerAsUser(
				containerName,
				'65532:65532',
				"touch /app/data/projects/runtime-write && stat -c '%u:%g' /app/data/projects/runtime-write"
			);
			expect(projectWrite).toBe('65532:65532');

			const buildsWrite = dockerProbeContainerAsUser(
				containerName,
				'65532:65532',
				"touch /builds/runtime-write && stat -c '%u:%g' /builds/runtime-write"
			);
			expect(buildsWrite).toBe('65532:65532');
		} finally {
			cleanupContainer(containerName);
			cleanupVolume(dataVolume);
			cleanupDir(projectsDir);
			cleanupDir(buildsDir);
		}
	});

	test('supports tcp docker host mode without a mounted Unix socket', async () => {
		const networkName = uniqueName('arcane-proxy-net');
		const proxyName = uniqueName('arcane-proxy');
		const containerName = uniqueName('arcane-proxy-app');
		const dataVolume = uniqueName('arcane-proxy-data');
		docker(['volume', 'create', dataVolume]);

		try {
			docker(['network', 'create', networkName], { stdio: 'inherit' });

			dockerRunContainer([
				'--name',
				proxyName,
				'--network',
				networkName,
				'--user',
				'0:0',
				'-v',
				'/var/run/docker.sock:/var/run/docker.sock:ro',
				SOCKET_PROXY_IMAGE,
				...SOCKET_PROXY_ARGUMENTS
			]);
			await expect
				.poll(() => dockerProxyRequest(networkName, proxyName, '/version'), {
					timeout: 30_000,
					intervals: [500, 1_000, 2_000]
				})
				.toContain('ApiVersion');
			expect(dockerProxyStatus(networkName, proxyName, '/v1.51/swarm')).toBe('403');

			dockerRunContainer([
				...defaultRunArgs(containerName, dataVolume),
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

			const dbStat = dockerProbeContainerAsUser(
				containerName,
				'1001:1001',
				"stat -c '%u:%g' /app/data/arcane.db"
			);
			expect(dbStat).toBe('1001:1001');

			const processStatuses = arcaneProcessStatuses(containerName);
			expect(
				processStatuses.some(
					(status) => status.pid === '1' && status.ppid === '0' && status.uid.startsWith('0\t')
				)
			).toBe(true);
			expect(
				processStatuses.some(
					(status) =>
						status.pid !== '1' &&
						status.ppid === '1' &&
						status.uid.startsWith('1001\t') &&
						status.gid.startsWith('1001\t')
				)
			).toBe(true);
		} finally {
			cleanupContainer(containerName);
			cleanupContainer(proxyName);
			cleanupNetwork(networkName);
			cleanupVolume(dataVolume);
		}
	});
});
