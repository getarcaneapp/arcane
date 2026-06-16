import { expect, test } from '@playwright/test';
import playwrightConfig from '../playwright.config';
import { createCLIConfig, runCLI, runCLIJSON, type CLIConfig } from '../utils/cli.util';
import { createMockOidcIssuer } from '../utils/oidc.util';
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

type Role = {
	id: string;
	name: string;
};

type FederatedCredential = {
	id: string;
};

type PlaywrightFederatedCredentialResponse = {
	credential: FederatedCredential;
};

type FederatedAuthOutput = {
	token: string;
	tokenType: string;
	expiresIn: number;
	issuedTokenType: string;
	source: string;
	persisted: boolean;
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

async function arcaneAPI<T>(path: string, init: RequestInit = {}): Promise<T> {
	const headers = new Headers(init.headers);
	headers.set('X-API-KEY', apiKey);
	if (init.body) {
		headers.set('Content-Type', headers.get('Content-Type') ?? 'application/json');
	}

	const response = await fetch(new URL(path, baseURL), { ...init, headers });
	if (!response.ok) {
		throw new Error(
			`${init.method ?? 'GET'} ${path} failed: ${response.status} ${await response.text()}`
		);
	}

	return (await response.json()) as T;
}

async function deleteFederatedCredential(id: string): Promise<void> {
	const response = await fetch(new URL(`/api/federated-credentials/${id}`, baseURL), {
		method: 'DELETE',
		headers: { 'X-API-KEY': apiKey }
	});
	if (!response.ok && response.status !== 404) {
		throw new Error(`failed to delete federated credential ${id}: ${response.status}`);
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

	test('auth federated exchanges a mock OIDC token and persists the CLI JWT', async () => {
		const issuer = await createMockOidcIssuer();
		const config = await createCLIConfig(baseURL, '');
		const subject = `repo:getarcaneapp/arcane:ref:refs/heads/e2e-${Date.now()}`;
		const audience = 'arcane-cli-e2e';
		let credentialID = '';

		try {
			const roles = await arcaneAPI<PaginatedResponse<Role>>('/api/roles?limit=100');
			const viewerRole = roles.data.find((role) => role.id === 'role_viewer');
			expect(viewerRole).toBeTruthy();

			const created = await arcaneAPI<PlaywrightFederatedCredentialResponse>(
				'/api/playwright/create-test-federated-credential',
				{
					method: 'POST',
					body: JSON.stringify({
						issuerUrl: issuer.issuerURL,
						audiences: [audience],
						subject,
						roleId: viewerRole!.id,
						tokenTtlSeconds: 600
					})
				}
			);
			credentialID = created.credential.id;

			const now = Math.floor(Date.now() / 1000);
			const subjectToken = issuer.token({
				iss: issuer.issuerURL,
				sub: subject,
				aud: audience,
				iat: now - 5,
				nbf: now - 5,
				exp: now + 300,
				jti: `cli-e2e-${now}`
			});

			const exchange = await runCommandJSON<FederatedAuthOutput>(config.configPath, [
				'auth',
				'federated',
				'--server',
				baseURL,
				'--audience',
				audience,
				'--token',
				subjectToken,
				'--persist',
				'--json'
			]);

			expect(exchange).toEqual(
				expect.objectContaining({
					token: expect.any(String),
					tokenType: 'Bearer',
					expiresIn: expect.any(Number),
					source: 'flag',
					persisted: true
				})
			);
			expect(exchange.expiresIn).toBeGreaterThan(0);

			const projects = await runCLIJSON<PaginatedResponse<{ id: string }>>(config.configPath, [
				'projects',
				'list',
				'--limit',
				'1'
			]);
			expect(Array.isArray(projects.data)).toBe(true);
		} finally {
			if (credentialID) {
				await deleteFederatedCredential(credentialID);
			}
			await config.cleanup();
			await issuer.close();
		}
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
				// API keys must carry at least one permission grant in the new
				// RBAC model — the CLI flag mirrors the backend's minItems:1
				// requirement. Pick a low-risk read-only perm for the e2e key.
				created = await runCLIJSON<CreatedApiKey>(config.configPath, [
					'admin',
					'api-keys',
					'create',
					name,
					'--description',
					'Created by CLI e2e',
					'--permission',
					'apikeys:list'
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

// Regression coverage for the remote-environment proxy authorization model.
//
// The manager proxies /api/environments/{id}/... to the target agent, which runs
// with a sudo permission set and performs no authorization of its own. The
// manager must therefore enforce the caller's per-environment permission BEFORE
// forwarding — otherwise any authenticated user could perform write actions on a
// remote environment regardless of role.
//
// These tests drive the real arcane-cli against a remote (edge) environment that
// has no connected agent. With no live tunnel, the outcome distinguishes the two
// states cleanly:
//   - a DENIED request returns 403 — the permission check runs before the tunnel
//     lookup, so it short-circuits regardless of connectivity;
//   - an AUTHORIZED request gets past the permission check and reaches the tunnel
//     stage, which returns 502 ("edge agent is not connected").
// The 403-vs-502 split is exactly what proves the permission is enforced per
// environment and is not a blanket block. The local environment ("0") is served
// directly (never proxied) and must remain unaffected.
test.describe('arcane-cli remote environment RBAC', () => {
	let adminKey = staticApiKey ?? '';
	let createdAdminKey = false;
	let remoteEnvId = '';
	let readonlyKey = '';
	let readonlyKeyId = '';

	async function adminFetch(path: string, init: RequestInit = {}): Promise<Response> {
		const headers = new Headers(init.headers);
		headers.set('X-API-KEY', adminKey);
		if (init.body) {
			headers.set('Content-Type', headers.get('Content-Type') ?? 'application/json');
		}
		return fetch(new URL(path, baseURL), { ...init, headers });
	}

	async function withAdminConfig<T>(
		environment: string,
		fn: (config: CLIConfig) => Promise<T>
	): Promise<T> {
		const config = await createCLIConfig(baseURL, adminKey, environment);
		try {
			return await fn(config);
		} finally {
			await config.cleanup();
		}
	}

	async function withReadonlyConfig<T>(
		environment: string,
		fn: (config: CLIConfig) => Promise<T>
	): Promise<T> {
		const config = await createCLIConfig(baseURL, readonlyKey, environment);
		try {
			return await fn(config);
		} finally {
			await config.cleanup();
		}
	}

	// runExpectingFailure runs the CLI expecting a non-zero exit, returning the
	// thrown error message (which includes stdout + stderr) for assertions. It
	// fails the test if the command unexpectedly succeeds.
	async function runExpectingFailure(configPath: string, args: string[]): Promise<string> {
		const message = await runCLI(configPath, args)
			.then(() => null)
			.catch((error: unknown) => (error instanceof Error ? error.message : String(error)));
		expect(message, `expected \`arcane-cli ${args.join(' ')}\` to fail`).toBeTruthy();
		return message as string;
	}

	test.beforeAll(async () => {
		if (!adminKey) {
			const created = await createTestApiKeys(1);
			adminKey = created.apiKeys[0].key;
			createdAdminKey = true;
		}

		const suffix = `${Date.now()}`.slice(-6);

		// Create a remote edge environment. No agent will ever connect to it; it
		// exists only so the proxy treats requests to it as remote (edge://) and
		// reaches either the permission check (403) or the tunnel lookup (502).
		const envResponse = await adminFetch('/api/environments', {
			method: 'POST',
			body: JSON.stringify({
				name: `cli-rbac-${suffix}`,
				apiUrl: `edge://cli-rbac-${suffix}`,
				isEdge: true,
				enabled: true
			})
		});
		expect(
			envResponse.ok,
			`failed to create remote env: ${envResponse.status} ${await envResponse.clone().text()}`
		).toBeTruthy();
		const envBody = (await envResponse.json()) as { data: { id: string } };
		remoteEnvId = envBody.data.id;
		expect(remoteEnvId).toBeTruthy();

		// Create a read-only scoped API key via the CLI: it can read volumes but
		// has no volumes:create grant, so the proxy must reject writes with it.
		await withAdminConfig('0', async (config) => {
			const created = await runCLIJSON<CreatedApiKey>(config.configPath, [
				'admin',
				'api-keys',
				'create',
				`cli-rbac-readonly-${suffix}`,
				'--description',
				'Read-only key for remote RBAC e2e',
				'--permission',
				'environments:read',
				'--permission',
				'volumes:list',
				'--permission',
				'volumes:read'
			]);
			readonlyKey = created.key;
			readonlyKeyId = created.id;
		});
		expect(readonlyKey).toMatch(/^arc_/);
	});

	test.afterAll(async () => {
		if (readonlyKeyId) {
			await adminFetch(`/api/api-keys/${readonlyKeyId}`, { method: 'DELETE' }).catch(
				() => undefined
			);
		}
		if (remoteEnvId) {
			await adminFetch(`/api/environments/${remoteEnvId}`, { method: 'DELETE' }).catch(
				() => undefined
			);
		}
		if (createdAdminKey) {
			await deleteTestApiKeys();
		}
	});

	test('denies a remote write for a read-only key (403)', async () => {
		await withReadonlyConfig(remoteEnvId, async (config) => {
			const error = await runExpectingFailure(config.configPath, [
				'volumes',
				'create',
				'--name',
				`cli-rbac-vol-${Date.now()}`
			]);
			expect(error).toContain('status 403');
		});
	});

	test('authorizes a remote write for an admin key, reaching the disconnected agent (502, not 403)', async () => {
		await withAdminConfig(remoteEnvId, async (config) => {
			const error = await runExpectingFailure(config.configPath, [
				'volumes',
				'create',
				'--name',
				`cli-rbac-vol-${Date.now()}`
			]);
			// The admin key passes the permission check, so the request reaches the
			// edge tunnel stage and fails only because no agent is connected.
			expect(error).toContain('status 502');
			expect(error).not.toContain('status 403');
		});
	});

	test('leaves the local environment unaffected for a read-only key', async () => {
		await withReadonlyConfig('0', async (config) => {
			const volumes = await runCLIJSON<{ data: unknown[] }>(config.configPath, [
				'volumes',
				'list',
				'--limit',
				'5'
			]);
			expect(Array.isArray(volumes.data)).toBe(true);
		});
	});
});
