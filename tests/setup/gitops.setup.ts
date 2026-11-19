import { chromium } from 'playwright';
import { expect } from 'playwright/test';
import type { TestProject } from 'vite-plus/test/node';
import { authStateFile, baseURL } from '../utils/report';

export default async function gitopsSetup(project: TestProject) {
	if (!project.vitest.projects.some((candidate) => candidate.name === 'chromium')) return;
	const endpoint = project.getProvidedContext().browserEndpoint;
	const browser = await (endpoint
		? chromium.connect(endpoint, { exposeNetwork: '<loopback>' })
		: chromium.launch());
	try {
		const page = await browser.newPage({ baseURL, storageState: authStateFile });
		const response = await page.request.post('/api/playwright/gitops', {
			timeout: 100_000
		});
		expect(response.status(), await response.text()).toBe(204);
		await page.goto('/projects');
		await expect(
			page.getByRole('link', { name: 'gitops-test-project', exact: true })
		).toBeVisible();
	} finally {
		await browser.close();
	}
}
