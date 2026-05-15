import { execFileSync, execSync } from 'child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

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
			'alpine',
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
	ensureProjectsDirIsContainerWritable(projectsDir);

	try {
		console.log('Building and starting Docker containers...');
		execSync(`docker compose -f ${composeFile} up -d --build`, { stdio: 'inherit' });
		console.log('Docker containers are up.');
	} catch (error) {
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
			const response = await fetch(baseURL);
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
		throw new Error(`Server at ${baseURL} did not become ready in time.`);
	}

	console.log('Global setup complete.\n');
}

export default globalSetup;
