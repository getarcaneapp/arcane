import { expect, test } from '@playwright/test';
import playwrightConfig from '../playwright.config';
import { createCLIConfig, runCLI, runCLIJSON, type CLIConfig } from '../utils/cli.util';
import { createTestApiKeys, deleteTestApiKeys } from '../utils/playwright.util';

type CreatedApiKey = {
	id: string;
	name: string;
	description?: string;
	key: string;
};

type PaginatedResponse<T> = {
	data: T[];
	pagination?: { totalItems?: number };
};

type JsonSmokeCommand = {
	name: string;
	args: string[];
	expectation: (value: unknown) => void;
};

const baseURL = String(playwrightConfig.use!.baseURL);
const staticApiKey = process.env.E2E_ADMIN_STATIC_API_KEY;
let apiKey = staticApiKey ?? '';

async function withConfig<T>(fn: (config: CLIConfig) => Promise<T>): Promise<T> {
	const config = await createCLIConfig(baseURL, apiKey);
	try {
		return await fn(config);
	} finally {
		await config.cleanup();
	}
}

async function runCommandJSON<T>(configPath: string, args: string[]): Promise<T> {
	const result = await runCLI(configPath, args);

	try {
		return JSON.parse(result.stdout) as T;
	} catch (error: unknown) {
		const message = error instanceof Error ? error.message : String(error);
		throw new Error(`failed to parse arcane-cli JSON output: ${message}\n\n${result.stdout}`);
	}
}

function expectPaginated(value: unknown): void {
	expect(value).toEqual(
		expect.objectContaining({
			data: expect.any(Array)
		})
	);
}

const readOnlyJsonSmokeCommands: JsonSmokeCommand[] = [
	{
		name: 'alerts',
		args: ['alerts', '--debug-all-good', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.objectContaining({ items: expect.any(Array) }));
		}
	},
	{
		name: 'images list',
		args: ['--output', 'json', 'images', 'list', '--limit', '5'],
		expectation: expectPaginated
	},
	{
		name: 'images counts',
		args: ['--output', 'json', 'images', 'counts'],
		expectation: (value) => {
			expect(value).toEqual(
				expect.objectContaining({
					data: expect.objectContaining({ totalImages: expect.any(Number) })
				})
			);
		}
	},
	{
		name: 'volumes list',
		args: ['volumes', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'volumes counts',
		args: ['volumes', 'counts', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.objectContaining({ total: expect.any(Number) }));
		}
	},
	{
		name: 'networks list',
		args: ['networks', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'networks counts',
		args: ['networks', 'counts', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.objectContaining({ total: expect.any(Number) }));
		}
	},
	{
		name: 'projects list',
		args: ['projects', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'projects counts',
		args: ['projects', 'counts', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Object));
		}
	},
	{
		name: 'settings list',
		args: ['settings', 'list', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Array));
		}
	},
	{
		name: 'settings public',
		args: ['settings', 'public', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Array));
		}
	},
	{
		name: 'jobs get',
		args: ['jobs', 'get', '--json'],
		expectation: (value) => {
			expect(value).toEqual(
				expect.objectContaining({ environmentHealthInterval: expect.any(String) })
			);
		}
	},
	{
		name: 'updater status',
		args: ['updater', 'status', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.objectContaining({ updatingContainers: expect.any(Number) }));
		}
	},
	{
		name: 'updater history',
		args: ['updater', 'history', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Array));
		}
	},
	{
		name: 'registries list',
		args: ['registries', 'list', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'repos list',
		args: ['repos', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'templates registries',
		args: ['templates', 'registries', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Array));
		}
	},
	{
		name: 'templates variables',
		args: ['templates', 'variables', '--json'],
		expectation: (value) => {
			expect(value === null || Array.isArray(value)).toBe(true);
		}
	},
	{
		name: 'admin users list',
		args: ['admin', 'users', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'admin events list',
		args: ['admin', 'events', 'list', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'admin events list-env',
		args: ['admin', 'events', 'list-env', '--limit', '5', '--json'],
		expectation: expectPaginated
	},
	{
		name: 'admin notifications apprise get',
		args: ['admin', 'notifications', 'apprise', 'get', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.objectContaining({ enabled: expect.any(Boolean) }));
		}
	},
	{
		name: 'admin notifications settings get',
		args: ['admin', 'notifications', 'settings', 'get', '--json'],
		expectation: (value) => {
			expect(value).toEqual(expect.any(Array));
		}
	}
];

test.describe('arcane-cli e2e', () => {
	test.beforeAll(async () => {
		if (staticApiKey) return;

		const response = await createTestApiKeys(1);
		apiKey = response.apiKeys[0].key;
	});

	test.afterAll(async () => {
		if (staticApiKey) return;

		await deleteTestApiKeys();
	});

	test('config set and show use an isolated config file', async () => {
		const config = await createCLIConfig('', '');
		try {
			await runCLI(config.configPath, [
				'config',
				'set',
				'server-url',
				baseURL,
				'api-key',
				apiKey,
				'environment',
				'0'
			]);

			const show = await runCLI(config.configPath, ['config', 'show']);
			expect(show.stdout).toContain(`Server URL:          ${baseURL}`);
			expect(show.stdout).toContain('API Key:             arc_');
			expect(show.stdout).toContain('Default Environment: 0');
		} finally {
			await config.cleanup();
		}
	});

	test('generated config quotes YAML metacharacters', async () => {
		const serverURL = `${baseURL}/path#fragment`;
		const config = await createCLIConfig(serverURL, `arc_test:'#{value}`);
		try {
			const show = await runCLI(config.configPath, ['config', 'show']);
			expect(show.stdout).toContain(`Server URL:          ${serverURL}`);
			expect(show.stdout).toContain('Default Environment: 0');
		} finally {
			await config.cleanup();
		}
	});

	test('doctor reports a healthy live connection as JSON', async () => {
		await withConfig(async (config) => {
			const report = await runCLIJSON<{
				healthy: boolean;
				checks: { name: string; status: string; details?: string }[];
			}>(config.configPath, ['doctor']);

			expect(report.healthy).toBe(true);
			expect(report.checks).toEqual(
				expect.arrayContaining([
					expect.objectContaining({ name: 'server_url', status: 'ok', details: baseURL }),
					expect.objectContaining({ name: 'auth', status: 'ok' }),
					expect.objectContaining({ name: 'api_connection', status: 'ok' })
				])
			);
		});
	});

	test('version returns server details as JSON', async () => {
		await withConfig(async (config) => {
			const version = await runCLIJSON<{
				currentVersion: string;
				displayVersion: string;
				updateAvailable: boolean;
			}>(config.configPath, ['version']);

			expect(version).toEqual(
				expect.objectContaining({
					currentVersion: expect.any(String),
					displayVersion: expect.any(String),
					updateAvailable: expect.any(Boolean)
				})
			);
			expect(version.currentVersion).toMatch(/^(v?\d+\.\d+\.\d+|dev)$/);
		});
	});

	test('environments list and get return local environment JSON', async () => {
		await withConfig(async (config) => {
			const environments = await runCLIJSON<PaginatedResponse<{ id: string; name: string }>>(
				config.configPath,
				['environments', 'list']
			);
			expect(environments.data).toEqual(
				expect.arrayContaining([expect.objectContaining({ id: '0' })])
			);

			const local = await runCLIJSON<{ id: string; name: string }>(config.configPath, [
				'environments',
				'get',
				'0'
			]);
			expect(local.id).toBe('0');
			expect(local.name).toBeTruthy();
		});
	});

	test('containers list uses the configured environment', async () => {
		await withConfig(async (config) => {
			const containers = await runCLIJSON<PaginatedResponse<{ id: string; name?: string }>>(
				config.configPath,
				['containers', 'list', '--limit', '5']
			);

			expect(Array.isArray(containers.data)).toBe(true);
			expect(containers.data.length).toBeLessThanOrEqual(5);
			expect(containers.pagination?.totalItems ?? containers.data.length).toBeGreaterThanOrEqual(
				containers.data.length
			);
		});
	});

	for (const command of readOnlyJsonSmokeCommands) {
		test(`${command.name} returns JSON`, async () => {
			await withConfig(async (config) => {
				const result = await runCommandJSON<unknown>(config.configPath, command.args);
				command.expectation(result);
			});
		});
	}

	test('admin api-keys create, get, update, and delete mutate through the CLI', async () => {
		await withConfig(async (config) => {
			const name = `cli-e2e-${Date.now()}`;
			const updatedName = `${name}-updated`;
			let created: CreatedApiKey | undefined;

			try {
				created = await runCLIJSON<CreatedApiKey>(config.configPath, [
					'admin',
					'api-keys',
					'create',
					name,
					'--description',
					'Created by CLI e2e'
				]);
				expect(created.id).toBeTruthy();
				expect(created.key).toMatch(/^arc_/);
				expect(created.name).toBe(name);

				const fetched = await runCLIJSON<{ id: string; name: string }>(config.configPath, [
					'admin',
					'api-keys',
					'get',
					created.id
				]);
				expect(fetched).toEqual(expect.objectContaining({ id: created.id, name }));

				await runCLIJSON(config.configPath, [
					'admin',
					'api-keys',
					'update',
					created.id,
					'--name',
					updatedName,
					'--description',
					'Updated by CLI e2e'
				]);

				const updated = await runCLIJSON<{ id: string; name: string }>(config.configPath, [
					'admin',
					'api-keys',
					'get',
					created.id
				]);
				expect(updated).toEqual(expect.objectContaining({ id: created.id, name: updatedName }));

				await runCLIJSON(config.configPath, ['--yes', 'admin', 'api-keys', 'delete', created.id]);

				const list = await runCLIJSON<PaginatedResponse<{ id: string }>>(config.configPath, [
					'admin',
					'api-keys',
					'list',
					'--limit',
					'100'
				]);
				expect(list.data.some((item) => item.id === created!.id)).toBe(false);
				created = undefined;
			} finally {
				if (created) {
					await runCLIJSON(config.configPath, [
						'--yes',
						'admin',
						'api-keys',
						'delete',
						created.id
					]).catch(() => undefined);
				}
			}
		});
	});
});
