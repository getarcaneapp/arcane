import { test, expect, type Page } from '../fixtures/test.fixture';
import { openRowActionsMenu } from '../utils/table-actions.util';

const ROUTES = {
	page: '/security',
	legacyVulnerabilities: '/images/vulnerabilities'
};

async function navigateToSecurity(page: Page) {
	await page.goto(ROUTES.page);
	await page.waitForLoadState('load');
}

function paginated<T>(data: T[]) {
	return {
		data,
		pagination: {
			totalPages: data.length > 0 ? 1 : 0,
			totalItems: data.length,
			currentPage: 1,
			itemsPerPage: 20
		}
	};
}

test.describe('Security Page', () => {
	test('redirects the old vulnerabilities URL to the security page', async ({ page }) => {
		await page.goto(ROUTES.legacyVulnerabilities);
		await page.waitForURL('**/security**');
		await expect(page.getByRole('heading', { name: 'Security', level: 1 })).toBeVisible();
	});

	test('shows the vulnerabilities and patches tabs', async ({ page }) => {
		await navigateToSecurity(page);

		await expect(page.getByRole('tab', { name: 'Vulnerabilities', exact: true })).toHaveAttribute(
			'data-state',
			'active'
		);
		await expect(page.getByRole('tab', { name: 'Patches', exact: true })).toBeVisible();
	});

	test('loads ignored vulnerabilities via the table switch', async ({ page }) => {
		await navigateToSecurity(page);

		const ignoredResponse = page.waitForResponse((response) => {
			const request = response.request();
			const url = new URL(response.url());
			return (
				request.method() === 'GET' &&
				url.pathname.endsWith('/vulnerabilities/all') &&
				url.searchParams.get('ignored') === 'true'
			);
		});

		await page.getByRole('switch', { name: 'Show ignored' }).click();
		const response = await ignoredResponse;
		expect(response.ok()).toBeTruthy();
		await expect(page.getByRole('switch', { name: 'Show ignored' })).toBeChecked();
	});

	test('reports a partial failure when starting scans for every image', async ({ page }) => {
		await navigateToSecurity(page);

		const scanRequests: string[] = [];
		await page.route(/\/api\/environments\/0\/images(?:\?.*)?$/, async (route) => {
			const requestUrl = new URL(route.request().url());
			expect(requestUrl.searchParams.get('limit')).toBe('1000');
			await route.fulfill({
				json: paginated([
					{ id: 'scan-success', repoTags: ['example/success:latest'] },
					{ id: 'scan-failure', repoTags: ['example/failure:latest'] }
				])
			});
		});
		await page.route(
			/\/api\/environments\/0\/images\/([^/]+)\/vulnerabilities\/scan$/,
			async (route) => {
				const imageId = new URL(route.request().url()).pathname.split('/').at(-3);
				if (!imageId) throw new Error('Scan request did not include an image ID');
				scanRequests.push(imageId);

				if (imageId === 'scan-failure') {
					await route.fulfill({ status: 500, json: { message: 'scanner unavailable' } });
					return;
				}

				await route.fulfill({
					json: {
						success: true,
						data: {
							imageId,
							imageName: 'example/success:latest',
							scanTime: new Date().toISOString(),
							status: 'scanning',
							activityId: 'scan-activity'
						}
					}
				});
			}
		);

		await page.getByRole('button', { name: 'Scan all images', exact: true }).click();

		await expect(
			page.getByText('Started 1 scans; 1 failed to start', { exact: true })
		).toBeVisible();
		expect(scanRequests.sort()).toEqual(['scan-failure', 'scan-success']);
	});

	test('ignores and restores a vulnerability from the row actions', async ({ page }) => {
		const vulnerability = {
			vulnerabilityId: 'CVE-2026-4242',
			pkgName: 'openssl',
			installedVersion: '3.0.0',
			fixedVersion: '3.0.1',
			severity: 'HIGH',
			imageId: 'security-image',
			imageName: 'example/security:latest'
		};
		let ignored = false;
		let ignorePayload: unknown;
		let unignoreRequestCount = 0;

		await page.route(/\/api\/environments\/0\/vulnerabilities\/summary$/, async (route) => {
			await route.fulfill({
				json: {
					success: true,
					data: {
						totalImages: 1,
						scannedImages: 1,
						summary: { critical: 0, high: 1, medium: 0, low: 0, unknown: 0, total: 1 }
					}
				}
			});
		});
		await page.route(/\/api\/environments\/0\/vulnerabilities\/all(?:\?.*)?$/, async (route) => {
			const showIgnored = new URL(route.request().url()).searchParams.get('ignored') === 'true';
			const rows =
				ignored === showIgnored
					? [{ ...vulnerability, ignored, ignoreId: ignored ? 'ignore-1' : undefined }]
					: [];
			await route.fulfill({ json: paginated(rows) });
		});
		await page.route(
			/\/api\/environments\/0\/vulnerabilities\/image-options(?:\?.*)?$/,
			async (route) => {
				await route.fulfill({ json: { success: true, data: [vulnerability.imageName] } });
			}
		);
		await page.route(/\/api\/environments\/0\/images\/patch-targets(?:\?.*)?$/, async (route) => {
			await route.fulfill({ json: paginated([]) });
		});
		await page.route(/\/api\/environments\/0\/vulnerabilities\/ignore$/, async (route) => {
			ignorePayload = route.request().postDataJSON();
			ignored = true;
			await route.fulfill({
				json: {
					success: true,
					data: { id: 'ignore-1', ...vulnerability }
				}
			});
		});
		await page.route(
			/\/api\/environments\/0\/vulnerabilities\/ignore\/ignore-1$/,
			async (route) => {
				expect(route.request().method()).toBe('DELETE');
				unignoreRequestCount += 1;
				ignored = false;
				await route.fulfill({ status: 204 });
			}
		);

		await navigateToSecurity(page);

		let vulnerabilityRow = page.getByRole('row').filter({ hasText: vulnerability.vulnerabilityId });
		let menu = await openRowActionsMenu(page, vulnerabilityRow);
		await menu.getByRole('menuitem', { name: 'Ignore vulnerability' }).click();

		await expect(
			page.getByText(`Ignored ${vulnerability.vulnerabilityId}`, { exact: true })
		).toBeVisible();
		await expect(vulnerabilityRow).toHaveCount(0);
		expect(ignorePayload).toEqual({
			imageId: vulnerability.imageId,
			vulnerabilityId: vulnerability.vulnerabilityId,
			pkgName: vulnerability.pkgName,
			installedVersion: vulnerability.installedVersion
		});

		await page.getByRole('switch', { name: 'Show ignored' }).click();
		vulnerabilityRow = page.getByRole('row').filter({ hasText: vulnerability.vulnerabilityId });
		await expect(vulnerabilityRow.getByText('Ignored', { exact: true })).toBeVisible();
		menu = await openRowActionsMenu(page, vulnerabilityRow);
		await menu.getByRole('menuitem', { name: 'Unignore', exact: true }).click();

		await expect(page.getByText('Vulnerability unignored', { exact: true })).toBeVisible();
		await expect(vulnerabilityRow).toHaveCount(0);
		expect(unignoreRequestCount).toBe(1);
	});

	test('starts a valid image patch and explains disabled patch actions', async ({ page }) => {
		const now = new Date().toISOString();
		let patchPayload: unknown;
		let patchStarted = false;
		const targets = () => [
			{
				imageId: 'remote-image',
				imageRef: 'example/remote:latest',
				fixableCount: 2,
				totalCount: 4,
				scanTime: now,
				...(patchStarted
					? {
							lastPatch: {
								id: 'patch-record',
								environmentId: '0',
								originalImageId: 'remote-image',
								originalRef: 'example/remote:latest',
								patchedRef: 'example/remote:arcane-patched',
								mode: 'buildkit',
								status: 'running',
								activityId: 'patch-activity',
								createdAt: now
							}
						}
					: {})
			},
			{
				imageId: 'local-image',
				imageRef: 'example/local:latest',
				fixableCount: 1,
				totalCount: 1,
				scanTime: now,
				localOnly: true
			},
			{
				imageId: 'clean-image',
				imageRef: 'example/clean:latest',
				fixableCount: 0,
				totalCount: 3,
				scanTime: now
			}
		];

		await page.route(/\/api\/environments\/0\/images\/patch-targets(?:\?.*)?$/, async (route) => {
			await route.fulfill({ json: paginated(targets()) });
		});
		await page.route(/\/api\/environments\/0\/images\/remote-image\/patch$/, async (route) => {
			patchPayload = route.request().postDataJSON();
			patchStarted = true;
			await route.fulfill({
				json: {
					success: true,
					data: {
						id: 'patch-record',
						environmentId: '0',
						originalImageId: 'remote-image',
						originalRef: 'example/remote:latest',
						patchedRef: 'example/remote:arcane-patched',
						mode: 'buildkit',
						status: 'running',
						activityId: 'patch-activity',
						createdAt: now
					}
				}
			});
		});

		await navigateToSecurity(page);
		await page.getByRole('tab', { name: 'Patches', exact: true }).click();

		const localRow = page.getByRole('row').filter({ hasText: 'example/local:latest' });
		let menu = await openRowActionsMenu(page, localRow);
		await expect(menu.getByRole('menuitem', { name: /Patch/ })).toBeDisabled();
		await expect(
			menu.getByText('Locally built image — rebuild it to update its packages')
		).toBeVisible();
		await page.keyboard.press('Escape');

		const cleanRow = page.getByRole('row').filter({ hasText: 'example/clean:latest' });
		menu = await openRowActionsMenu(page, cleanRow);
		await expect(menu.getByRole('menuitem', { name: /Patch/ })).toBeDisabled();
		await expect(menu.getByText('The last scan found no fixable vulnerabilities')).toBeVisible();
		await page.keyboard.press('Escape');

		const remoteRow = page.getByRole('row').filter({ hasText: 'example/remote:latest' });
		menu = await openRowActionsMenu(page, remoteRow);
		await menu.getByRole('menuitem', { name: 'Patch', exact: true }).click();

		await expect(
			page.getByText('Patching image; the result will be tagged example/remote:arcane-patched', {
				exact: true
			})
		).toBeVisible();
		expect(patchPayload).toEqual({ scanId: 'remote-image' });
		await expect(remoteRow.getByText('Running', { exact: true })).toBeVisible();
	});
});
