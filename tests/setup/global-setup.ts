import { execFileSync } from 'child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export function captureComposeDiagnostics(composeFile: string) {
	const directory = path.resolve(__dirname, '../test-results/compose');
	fs.mkdirSync(directory, { recursive: true });
	for (const [name, args] of [
		['services', ['ps', '--all', '--format', 'json']],
		['logs', ['logs', '--no-color', '--timestamps']]
	] as const) {
		try {
			const output = execFileSync('docker', ['compose', '-f', composeFile, ...args], {
				encoding: 'utf8',
				maxBuffer: 32 * 1024 * 1024
			});
			fs.writeFileSync(path.join(directory, `${name}.txt`), output);
		} catch (error) {
			fs.writeFileSync(path.join(directory, `${name}.txt`), `Capture failed: ${String(error)}\n`);
			console.error(`Failed to capture Compose ${name}:`, error);
		}
	}
}

function ensureProjectsDirIsContainerWritable(projectsDir: string) {
	fs.mkdirSync(projectsDir, { recursive: true });

	try {
		fs.accessSync(projectsDir, fs.constants.R_OK | fs.constants.W_OK | fs.constants.X_OK);
		fs.chmodSync(projectsDir, 0o777);
		return;
	} catch (error) {
		console.warn(
			'Projects directory is not writable by the test runner, fixing permissions:',
			error
		);
	}

	execFileSync(
		'docker',
		[
			'run',
			'--rm',
			'-v',
			`${projectsDir}:/projects`,
			'public.ecr.aws/docker/library/alpine:3.20',
			'sh',
			'-c',
			'chmod -R 777 /projects'
		],
		{ stdio: 'inherit' }
	);
}

async function globalSetup() {
	console.log('\nStarting global setup...');

	const composeFile = process.env.COMPOSE_FILE
		? path.resolve(__dirname, '..', process.env.COMPOSE_FILE)
		: path.resolve(__dirname, 'compose.yaml');

	// This directory is bind-mounted into Arcane at /app/data/projects. Create it
	// before `docker compose up` so Docker does not create it as root, and make it
	// writable for the hardened non-root runtime user (65532).
	const projectsDir = path.resolve(__dirname, 'projects');

	try {
		ensureProjectsDirIsContainerWritable(projectsDir);
		const staticProjectDir = path.join(projectsDir, 'test-project-static');
		fs.accessSync(path.join(staticProjectDir, 'compose.yaml'), fs.constants.R_OK);
		const staticEnvFile = path.join(staticProjectDir, '.env');
		if (!fs.existsSync(staticEnvFile)) fs.writeFileSync(staticEnvFile, '');
		for (const image of [
			'public.ecr.aws/docker/library/busybox:1.37',
			'public.ecr.aws/docker/library/alpine:3.20',
			'public.ecr.aws/nginx/nginx:stable-alpine'
		]) {
			try {
				execFileSync('docker', ['image', 'inspect', image], { stdio: 'ignore' });
			} catch {
				execFileSync('docker', ['pull', image], { stdio: 'inherit' });
			}
		}
		console.log('Building and starting Docker containers...');
		execFileSync('docker', ['compose', '-f', composeFile, 'up', '-d', '--build'], {
			stdio: 'inherit'
		});
		console.log('Docker containers are up.');
	} catch (error) {
		captureComposeDiagnostics(composeFile);
		console.error('Failed to start Docker containers:', error);
		throw error;
	}

	// 2. Wait for the server to be ready
	const baseURL = process.env.BASE_URL || 'http://localhost:3000';
	console.log(`Waiting for server at ${baseURL}...`);

	const maxAttempts = 60;
	let attempts = 0;
	while (attempts < maxAttempts) {
		try {
			const response = await fetch(new URL('/api/health', baseURL), {
				signal: AbortSignal.timeout(2000)
			});
			if (response.ok) {
				console.log('Server is ready!');
				break;
			}
		} catch (e) {
			// Ignore connection errors
		}
		attempts++;
		await new Promise((resolve) => setTimeout(resolve, 2000));
	}

	if (attempts === maxAttempts) {
		captureComposeDiagnostics(composeFile);
		throw new Error(`Server at ${baseURL} did not become ready in time.`);
	}

	console.log('Global setup complete.\n');
}

export default globalSetup;
