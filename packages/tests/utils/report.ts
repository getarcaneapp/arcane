import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const reportDirectory = fileURLToPath(new URL('../.report/', import.meta.url));
export const authStateFile = path.join(reportDirectory, 'auth', 'login.json');
