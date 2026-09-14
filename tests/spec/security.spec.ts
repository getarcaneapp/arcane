import { readFile } from 'node:fs/promises';
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

function vulnerabilityResponse(response: { url(): string; request(): { method(): string } }) {
	const url = new URL(response.url());
	return response.request().method() === 'GET' && url.pathname.endsWith('/vulnerabilities/all')
		? url.searchParams
		: null;
}

async function mockSecurityPageData(page: Page, imageNames: string[]) {
	await page.route(/\/api\/environments\/[^/]+\/vulnerabilities\/summary$/, async (route) => {
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
	await page.route(
		/\/api\/environments\/[^/]+\/vulnerabilities\/image-options(?:\?.*)?$/,
		async (route) => {
			await route.fulfill({ json: { success: true, data: imageNames } });
		}
	);
	await page.route(/\/api\/environments\/[^/]+\/images\/patch-targets(?:\?.*)?$/, async (route) => {
		await route.fulfill({ json: paginated([]) });
	});
}

const EXPORT_FILENAME = 'vulnerabilities-0-2026-09-13T10-00-00Z.csv';
const EXPORT_BODY =
	'﻿CVE,Severity,Package,Installed Version,Fixed Version,Image,Ignored\r\nCVE-2026-0001,HIGH,openssl,3.0.0,3.0.1,example/security:latest,false\r\n';

function fulfillExport(route: { fulfill(options: object): Promise<void> }) {
	return route.fulfill({
		status: 200,
		contentType: 'text/csv; charset=utf-8',
		headers: { 'content-disposition': `attachment; filename="${EXPORT_FILENAME}"` },
		body: EXPORT_BODY
	});
}

function exportButton(page: Page) {
	return page.getByRole('button', { name: /Export CSV/ });
}

async function exportCsv(page: Page) {
	const downloadPromise = page.waitForEvent('download');
	await exportButton(page).click();
	const download = await downloadPromise;
	expect(download.suggestedFilename()).toBe(EXPORT_FILENAME);
	const path = await download.path();
	if (!path) throw new Error('Download did not produce a file');
	return readFile(path, 'utf8');
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

	test('exports every matching vulnerability from any page through the backend', async ({
		page
	}) => {
		const image = 'example/security:latest';
		const rows = ['CVE-2026-0001', 'CVE-2026-0002', 'CVE-2026-0003', 'CVE-2026-0004'].map(
			(vulnerabilityId) => ({
				vulnerabilityId,
				pkgName: 'openssl',
				installedVersion: '3.0.0',
				fixedVersion: '3.0.1',
				severity: 'HIGH',
				imageId: 'security-image',
				imageName: image
			})
		);
		const listRequests: URLSearchParams[] = [];
		const exportRequests: URLSearchParams[] = [];

		await mockSecurityPageData(page, [image]);
		await page.route(/\/api\/environments\/0\/vulnerabilities\/all(?:\?.*)?$/, async (route) => {
			const params = new URL(route.request().url()).searchParams;
			listRequests.push(params);
			const secondPage = params.get('start') === '20';
			await route.fulfill({
				json: {
					data: secondPage ? rows.slice(2) : rows.slice(0, 2),
					pagination: {
						totalPages: 2,
						totalItems: 22,
						currentPage: secondPage ? 2 : 1,
						itemsPerPage: 20
					}
				}
			});
		});
		await page.route(/\/api\/environments\/0\/vulnerabilities\/export(?:\?.*)?$/, async (route) => {
			exportRequests.push(new URL(route.request().url()).searchParams);
			await fulfillExport(route);
		});

		await navigateToSecurity(page);
		const panel = page.getByRole('tabpanel', { name: 'Vulnerabilities' });
		await expect(page.getByRole('row').filter({ hasText: 'CVE-2026-0001' })).toBeVisible();

		const tableRequest = (matches: (params: URLSearchParams) => boolean) =>
			page.waitForResponse((response) => {
				const params = vulnerabilityResponse(response);
				return params !== null && matches(params);
			});

		let refreshed = tableRequest((params) => params.get('search') === 'openssl');
		await panel.getByPlaceholder('Search…').fill('openssl');
		await refreshed;

		refreshed = tableRequest((params) => params.get('severity') === 'HIGH');
		await panel.getByTestId('facet-severity-trigger').click();
		await page.getByTestId('facet-severity-option-HIGH').click();
		await refreshed;
		await page.keyboard.press('Escape');

		refreshed = tableRequest((params) => params.get('imageName') === image);
		await panel.getByTestId('facet-image-trigger').click();
		await page.getByTestId(`facet-image-option-${image}`).click();
		await refreshed;
		await page.keyboard.press('Escape');

		refreshed = tableRequest(
			(params) => params.get('sort') === 'imageName' && params.get('order') === 'desc'
		);
		await panel.getByRole('columnheader', { name: 'Image' }).getByRole('button').click();
		await page.getByRole('menuitem', { name: 'Desc', exact: true }).click();
		await refreshed;

		refreshed = tableRequest((params) => params.get('start') === '20');
		await panel.getByRole('button', { name: 'Go to next page', exact: true }).click();
		await refreshed;
		await expect(page.getByRole('row').filter({ hasText: 'CVE-2026-0003' })).toBeVisible();

		const listRequestsBeforeExport = listRequests.length;
		const content = await exportCsv(page);

		expect(content).toBe(EXPORT_BODY);
		expect(exportRequests).toHaveLength(1);
		expect(Object.fromEntries(exportRequests[0])).toEqual({
			search: 'openssl',
			severity: 'HIGH',
			imageName: image,
			sort: 'imageName',
			order: 'desc'
		});

		await expect(page.getByRole('row').filter({ hasText: 'CVE-2026-0003' })).toBeVisible();
		await expect(page.getByRole('row').filter({ hasText: 'CVE-2026-0001' })).toHaveCount(0);
		await expect(panel.getByPlaceholder('Search…')).toHaveValue('openssl');
		expect(listRequests).toHaveLength(listRequestsBeforeExport);
	});

	test('exports only ignored vulnerabilities when the ignored switch is on', async ({ page }) => {
		let exportRequest: URLSearchParams | undefined;

		await mockSecurityPageData(page, []);
		await page.route(/\/api\/environments\/0\/vulnerabilities\/all(?:\?.*)?$/, async (route) => {
			await route.fulfill({ json: paginated([]) });
		});
		await page.route(/\/api\/environments\/0\/vulnerabilities\/export(?:\?.*)?$/, async (route) => {
			exportRequest = new URL(route.request().url()).searchParams;
			await fulfillExport(route);
		});

		await navigateToSecurity(page);
		const ignoredResponse = page.waitForResponse(
			(response) => vulnerabilityResponse(response)?.get('ignored') === 'true'
		);
		await page.getByRole('switch', { name: 'Show ignored' }).click();
		await ignoredResponse;

		await exportCsv(page);
		expect(exportRequest?.get('ignored')).toBe('true');
		expect(exportRequest?.has('start')).toBe(false);
		expect(exportRequest?.has('limit')).toBe(false);
	});

	test('blocks duplicate exports and recovers after a failed export', async ({ page }) => {
		let exportRequests = 0;
		let mode: 'hold' | 'fail' | 'ok' = 'hold';
		let releaseExport = () => {};
		const held = new Promise<void>((resolve) => {
			releaseExport = resolve;
		});

		await mockSecurityPageData(page, []);
		await page.route(/\/api\/environments\/0\/vulnerabilities\/all(?:\?.*)?$/, async (route) => {
			await route.fulfill({ json: paginated([]) });
		});
		await page.route(/\/api\/environments\/0\/vulnerabilities\/export(?:\?.*)?$/, async (route) => {
			exportRequests += 1;
			if (mode === 'hold') await held;
			if (mode === 'fail') {
				await route.fulfill({ status: 500, json: { message: 'export unavailable' } });
				return;
			}
			await fulfillExport(route);
		});

		await navigateToSecurity(page);
		const button = exportButton(page);
		await expect(button).toBeEnabled();

		const firstDownload = page.waitForEvent('download');
		await button.click();
		await expect(button).toBeDisabled();
		await button.click({ force: true, timeout: 1000 }).catch(() => undefined);
		releaseExport();
		await firstDownload;
		await expect(button).toBeEnabled();
		expect(exportRequests).toBe(1);

		mode = 'fail';
		await button.click();
		await expect(page.getByText('Failed to export vulnerabilities', { exact: true })).toBeVisible();
		await expect(button).toBeEnabled();

		mode = 'ok';
		await exportCsv(page);
	});

	test('discards an export that finishes after switching environments', async ({ page }) => {
		const localEnvironment = {
			id: '0',
			name: 'Local Test',
			apiUrl: 'unix:///var/run/docker.sock',
			status: 'online',
			enabled: true,
			isEdge: false
		};
		const remoteEnvironment = {
			id: 'remote-export-test',
			name: 'Remote Test',
			apiUrl: 'https://remote.example.invalid',
			status: 'online',
			enabled: true,
			isEdge: false
		};
		let releaseExport = () => {};
		const held = new Promise<void>((resolve) => {
			releaseExport = resolve;
		});

		await page.addInitScript(() => {
			localStorage.removeItem('selectedEnvironmentId');
		});
		await page.route(/\/api\/environments(?:\?.*)?$/, async (route) => {
			await route.fulfill({ json: paginated([localEnvironment, remoteEnvironment]) });
		});
		await page.route(/\/api\/stream(?:\?.*)?$/, async (route) => {
			const channels =
				new URL(route.request().url()).searchParams.get('channels')?.split(',') ?? [];
			const timestamp = new Date().toISOString();
			await route.fulfill({
				status: 200,
				contentType: 'application/x-json-stream',
				body: channels.includes('environments')
					? `${JSON.stringify({
							channel: 'environments',
							environment: {
								type: 'snapshot',
								environments: [localEnvironment, remoteEnvironment],
								timestamp
							},
							timestamp
						})}\n`
					: ''
			});
		});
		await page.route(
			/\/api\/environments\/remote-export-test\/settings(?:\/public)?$/,
			async (route) => {
				await route.fulfill({
					json: [{ key: 'featureVulnerabilityManagementEnabled', value: 'true' }]
				});
			}
		);
		await mockSecurityPageData(page, []);
		await page.route(
			/\/api\/environments\/[^/]+\/vulnerabilities\/all(?:\?.*)?$/,
			async (route) => {
				await route.fulfill({ json: paginated([]) });
			}
		);
		await page.route(/\/api\/environments\/0\/vulnerabilities\/export(?:\?.*)?$/, async (route) => {
			await held;
			await fulfillExport(route);
		});

		await navigateToSecurity(page);
		await exportButton(page).click();

		await page.getByRole('button').filter({ hasText: localEnvironment.name }).first().click();
		const dialog = page.getByRole('dialog', { name: 'Select Environment' });
		await expect(dialog).toBeVisible();
		await dialog.getByRole('button').filter({ hasText: remoteEnvironment.name }).first().click();
		await expect(
			page.getByRole('button').filter({ hasText: remoteEnvironment.name }).first()
		).toBeVisible();

		releaseExport();
		const download = await page.waitForEvent('download', { timeout: 1500 }).catch(() => null);
		expect(download).toBeNull();
		await expect(page.getByText('Failed to export vulnerabilities', { exact: true })).toHaveCount(
			0
		);
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

test.describe('Disabled vulnerability management', () => {
	test.beforeEach(async ({ page }) => {
		await page.route(/\/api\/environments\/[^/]+\/settings\/public$/, async (route) => {
			await route.fulfill({
				json: [{ key: 'featureVulnerabilityManagementEnabled', value: 'false' }]
			});
		});
	});

	for (const path of [ROUTES.page, ROUTES.legacyVulnerabilities]) {
		test(`blocks vulnerability data requests from ${path}`, async ({ page }) => {
			const vulnerabilityRequests: string[] = [];
			page.on('request', (request) => {
				if (
					/\/api\/environments\/[^/]+\/(?:images\/[^/]+\/)?vulnerabilities(?:\/|$)/.test(
						new URL(request.url()).pathname
					)
				) {
					vulnerabilityRequests.push(request.url());
				}
			});

			await page.goto(path);
			await expect(
				page.getByText(
					'Vulnerability management is disabled for this environment. Saved reports and settings are retained.',
					{ exact: true }
				)
			).toBeVisible();
			await expect(page.getByRole('tab', { name: 'Vulnerabilities', exact: true })).toHaveCount(0);
			await expect(page.getByRole('button', { name: 'Scan all images', exact: true })).toHaveCount(
				0
			);
			await expect(page.getByRole('button', { name: 'Features', exact: true })).toBeVisible();
			expect(vulnerabilityRequests).toEqual([]);
		});
	}

	test('hides image vulnerability controls and keeps standalone patching', async ({ page }) => {
		await page.route(/\/api\/environments\/0\/images(?:\?.*)?$/, async (route) => {
			await route.fulfill({
				json: paginated([
					{
						id: 'disabled-feature-image',
						repo: 'example/feature-test',
						tag: 'latest',
						repoTags: ['example/feature-test:latest'],
						repoDigests: ['example/feature-test@sha256:1234'],
						size: 1024,
						created: 1700000000,
						inUse: false,
						vulnerabilityScan: { status: 'completed', summary: { total: 9, critical: 9 } }
					}
				])
			});
		});

		await page.goto('/images');
		const row = page.getByRole('row').filter({ hasText: 'example/feature-test' });
		await expect(row).toBeVisible();
		await expect(
			page.getByRole('columnheader', { name: 'Vulnerabilities', exact: true })
		).toHaveCount(0);
		const menu = await openRowActionsMenu(page, row);
		await expect(menu.getByRole('menuitem', { name: 'Scan', exact: true })).toHaveCount(0);
		await expect(menu.getByRole('menuitem', { name: 'Patch', exact: true })).toBeVisible();
	});
});
