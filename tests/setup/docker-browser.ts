import { expect, type FullConfig } from '@playwright/test';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';

export default async function dockerBrowserSetup(config: FullConfig) {
	if (
		process.env.PLAYWRIGHT_DOCKER_BROWSER === '0' ||
		process.env.PW_TEST_CONNECT_WS_ENDPOINT ||
		config.projects.every((project) => project.use.connectOptions) ||
		(process.platform !== 'linux' && process.env.PLAYWRIGHT_DOCKER_BROWSER !== '1')
	)
		return;
	const version = createRequire(import.meta.url)('@playwright/test/package.json').version as string;
	const name = `arcane-e2e-browser-${process.pid}-${Date.now()}`;
	let created = false;
	const cleanup = () => {
		delete process.env.ARCANE_PLAYWRIGHT_WS_ENDPOINT;
		if (created) {
			const directory = path.join(config.rootDir, 'test-results', 'browser');
			try {
				mkdirSync(directory, { recursive: true });
				const logs = spawnSync('docker', ['logs', name], {
					encoding: 'utf8',
					maxBuffer: 4 * 1024 * 1024
				});
				writeFileSync(path.join(directory, 'logs.txt'), `${logs.stdout ?? ''}${logs.stderr ?? ''}`);
				if (logs.error || logs.status !== 0)
					throw new Error(`Capture browser logs: ${logs.error ?? logs.stderr}`);
				writeFileSync(
					path.join(directory, 'state.json'),
					execFileSync('docker', ['inspect', '--format', '{{json .State}}', name], {
						encoding: 'utf8'
					})
				);
			} finally {
				execFileSync('docker', ['rm', '-f', name], { stdio: 'pipe' });
			}
		}
	};
	try {
		// Docker changes host interfaces throughout these tests. Keep browsers on their own bridge.
		execFileSync(
			'docker',
			[
				'run',
				'-d',
				'--name',
				name,
				'--init',
				'--ipc=host',
				'--network',
				'bridge',
				'--publish',
				'127.0.0.1::3000',
				`mcr.microsoft.com/playwright:v${version}-noble`,
				'npx',
				'-y',
				`playwright@${version}`,
				'run-server',
				'--port',
				'3000',
				'--host',
				'0.0.0.0'
			],
			{ encoding: 'utf8', timeout: 120_000 }
		);
		created = true;
		const address = execFileSync('docker', ['port', name, '3000/tcp'], {
			encoding: 'utf8'
		}).trim();
		expect(address).toMatch(/^127\.0\.0\.1:\d+$/);
		await expect
			.poll(
				async () => {
					try {
						return (await fetch(`http://${address}/`, { signal: AbortSignal.timeout(2000) })).ok;
					} catch {
						return false;
					}
				},
				{ timeout: 90_000, message: 'Playwright browser server must be ready' }
			)
			.toBe(true);
		process.env.ARCANE_PLAYWRIGHT_WS_ENDPOINT = `ws://${address}/`;
		console.log(`Playwright browser server ready: ${name}`);
	} catch (error) {
		try {
			cleanup();
		} catch (cleanupError) {
			throw new AggregateError([error, cleanupError], 'Browser setup and cleanup failed');
		}
		throw error;
	}
	return cleanup;
}
