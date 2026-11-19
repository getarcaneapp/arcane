import { chromium } from 'playwright';
import type { TestProject } from 'vite-plus/test/node';
import authUtil from '../utils/auth.util';
import { authStateFile, baseURL } from '../utils/report';

export default async function authSetup(project: TestProject) {
	if (project.vitest.projects.every((candidate) => candidate.name === 'cli')) return;
	const endpoint = project.getProvidedContext().browserEndpoint;
	const browser = await (endpoint
		? chromium.connect(endpoint, { exposeNetwork: '<loopback>' })
		: chromium.launch());
	try {
		const page = await browser.newPage({ baseURL });
		const currentPassword = await authUtil.login(page);
		await page.waitForURL('/dashboard');
		await authUtil.changeDefaultPassword(page, currentPassword, authUtil.TEST_PASSWORD);
		await page.context().storageState({ path: authStateFile });
	} finally {
		await browser.close();
	}
}
