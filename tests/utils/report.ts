import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const reportDirectory = fileURLToPath(new URL('../.report/', import.meta.url));
export const authStateFile = path.join(reportDirectory, 'auth', 'login.json');
export const baseURL = process.env.E2E_BASE_URL || 'http://localhost:3000';
export const composeFile = fileURLToPath(
	new URL(`../${process.env.COMPOSE_FILE || 'setup/compose.yaml'}`, import.meta.url)
);
