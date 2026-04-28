import { execFile } from 'node:child_process';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, '..', '..');
const cliDir = path.join(repoRoot, 'cli');
const cliBinDir = path.join(repoRoot, 'tests', '.bin');
const cliBinName = process.platform === 'win32' ? 'arcane-cli.exe' : 'arcane-cli';
const cliBinPath = path.join(cliBinDir, cliBinName);

let buildPromise: Promise<string> | undefined;

export type CLIConfig = {
	configPath: string;
	cleanup: () => Promise<void>;
};

export type CLICommandResult = {
	stdout: string;
	stderr: string;
};

function quoteYAMLString(value: string): string {
	return `'${value.replaceAll("'", "''")}'`;
}

function getErrorField(error: unknown, field: 'code' | 'signal' | 'stdout' | 'stderr'): unknown {
	if (typeof error !== 'object' || error === null || !(field in error)) {
		return undefined;
	}

	return (error as Record<typeof field, unknown>)[field];
}

export async function buildCLI(): Promise<string> {
	if (!buildPromise) {
		buildPromise = (async () => {
			await fs.mkdir(cliBinDir, { recursive: true });
			await execFileAsync('go', ['build', '-o', cliBinPath, '.'], {
				cwd: cliDir,
				maxBuffer: 1024 * 1024 * 10
			});
			return cliBinPath;
		})();
	}

	return buildPromise;
}

export async function createCLIConfig(
	serverURL: string,
	apiKey: string,
	environment = '0'
): Promise<CLIConfig> {
	const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'arcane-cli-e2e-'));
	const configPath = path.join(dir, 'arcanecli.yml');
	const content = [
		`server_url: ${quoteYAMLString(serverURL)}`,
		`api_key: ${quoteYAMLString(apiKey)}`,
		`default_environment: "${environment}"`,
		'log_level: info',
		''
	].join('\n');

	await fs.writeFile(configPath, content, { mode: 0o600 });

	return {
		configPath,
		cleanup: () => fs.rm(dir, { recursive: true, force: true })
	};
}

export async function runCLI(
	configPath: string,
	args: string[],
	options: { timeoutMs?: number } = {}
): Promise<CLICommandResult> {
	const binary = await buildCLI();
	const commandArgs = ['--config', configPath, '--no-color', ...args];

	try {
		const result = await execFileAsync(binary, commandArgs, {
			cwd: repoRoot,
			env: { ...process.env, NO_COLOR: '1' },
			timeout: options.timeoutMs ?? 30000,
			maxBuffer: 1024 * 1024 * 10
		});
		return { stdout: result.stdout, stderr: result.stderr };
	} catch (error: unknown) {
		const rawStdout = getErrorField(error, 'stdout');
		const rawStderr = getErrorField(error, 'stderr');
		const rawCode = getErrorField(error, 'code') ?? getErrorField(error, 'signal') ?? 'unknown';
		const stdout = typeof rawStdout === 'string' ? rawStdout : '';
		const stderr = typeof rawStderr === 'string' ? rawStderr : '';
		const code = typeof rawCode === 'string' || typeof rawCode === 'number' ? rawCode : 'unknown';
		throw new Error(
			[
				`arcane-cli failed with exit ${code}`,
				`command: ${binary} ${commandArgs.join(' ')}`,
				`stdout:\n${stdout}`,
				`stderr:\n${stderr}`
			].join('\n\n')
		);
	}
}

export async function runCLIJSON<T>(
	configPath: string,
	args: string[],
	options?: { timeoutMs?: number }
): Promise<T> {
	const result = await runCLI(configPath, ['--output', 'json', ...args], options);

	try {
		return JSON.parse(result.stdout) as T;
	} catch (error: unknown) {
		const message = error instanceof Error ? error.message : String(error);
		throw new Error(`failed to parse arcane-cli JSON output: ${message}\n\n${result.stdout}`);
	}
}
