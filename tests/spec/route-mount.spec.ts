import { expect, test } from '../fixtures/test.fixture';

const AUTHENTICATED_ROUTES = [
	'/account',
	'/containers',
	'/containers/new',
	'/customize',
	'/customize/git-repositories',
	'/customize/registries',
	'/customize/templates',
	'/customize/templates/create',
	'/customize/templates/default',
	'/customize/variables',
	'/dashboard',
	'/environments',
	'/environments/0',
	'/environments/0/gitops',
	'/events',
	'/images',
	'/images/builds',
	'/networks',
	'/networks/ports',
	'/networks/topology',
	'/no-access',
	'/projects',
	'/projects/new',
	'/security',
	'/settings',
	'/settings/activity',
	'/settings/api-keys',
	'/settings/authentication',
	'/settings/backups',
	'/settings/backups/s3',
	'/settings/builds',
	'/settings/diagnostics',
	'/settings/notifications',
	'/settings/roles',
	'/settings/roles/new',
	'/settings/timeouts',
	'/settings/users',
	'/settings/webhooks',
	'/swarm/cluster',
	'/updates',
	'/volumes'
] as const;

const LEGACY_REDIRECTS = [
	{ from: '/ports?search=8080', to: '/networks/ports?search=8080' },
	{ from: '/images/vulnerabilities?tab=ignored', to: '/security?tab=vulnerabilities' },
	{ from: '/settings/jobs', to: '/environments/0?tab=jobs' }
] as const;

test.describe('authenticated route mounts', () => {
	for (const route of AUTHENTICATED_ROUTES) {
		test(`${route} mounts without the application error view`, async ({ page }) => {
			const response = await page.goto(route);

			expect(response, `Expected ${route} to return a document response`).not.toBeNull();
			expect(response!.status(), `Expected ${route} not to return a server error`).toBeLessThan(
				500
			);
			await expect(page.locator('main').first()).toBeVisible();
			await expect(page.getByText('Something went wrong', { exact: true })).toHaveCount(0);
			await expect(
				page.getByText('Unable to connect to local Docker daemon', { exact: true })
			).toHaveCount(0);
			await expect(page.getByText(/^Unable to connect to remote environment /)).toHaveCount(0);
		});
	}
});

test.describe('legacy route redirects', () => {
	for (const { from, to } of LEGACY_REDIRECTS) {
		test(`${from} redirects to ${to}`, async ({ page }) => {
			await page.goto(from);
			await expect(page).toHaveURL(to);
		});
	}
});
