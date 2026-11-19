import path from 'node:path';
import { devices } from 'playwright';
import { defineConfig } from 'vite-plus';
import { authStateFile, reportDirectory } from './utils/report';

const ci = !!process.env.CI;
const shared = {
	testTimeout: 30_000,
	hookTimeout: 180_000,
	retry: ci ? 2 : 0
};
const responsive = {
	...shared,
	include: ['spec/responsive.spec.ts']
};

// fmt/lint config lives in the root vite.config.ts (Vite+ resolves them from the workspace root).
export default defineConfig({
	test: {
		globalSetup: [
			'./setup/docker-browser.ts',
			'./setup/global-setup.ts',
			'./setup/auth.setup.ts',
			'./setup/gitops.setup.ts'
		],
		fileParallelism: false,
		allowOnly: !ci,
		bail: ci ? 20 : 0,
		reporters: [
			'./utils/compose-reporter.ts',
			'default',
			['html', { outputDir: path.join(reportDirectory, 'html') }],
			[
				'junit',
				{
					outputFile: path.join(
						reportDirectory,
						path.basename(process.env.E2E_JUNIT_OUTPUT_FILE || 'junit.xml')
					)
				}
			],
			...(ci ? ['github-actions'] : [])
		],
		projects: [
			{
				test: {
					...shared,
					name: 'chromium',
					include: ['spec/*.spec.ts'],
					exclude: ['spec/cli.spec.ts', 'spec/responsive.spec.ts', 'spec/accessibility.spec.ts'],
					provide: {
						contextOptions: { ...devices['Desktop Chrome'], storageState: authStateFile }
					}
				}
			},
			{
				test: {
					...responsive,
					name: 'mobile-chromium',
					provide: {
						contextOptions: { ...devices['Pixel 7'], storageState: authStateFile }
					}
				}
			},
			{
				test: {
					...responsive,
					name: 'tablet-chromium',
					provide: {
						contextOptions: {
							...devices['Desktop Chrome'],
							viewport: { width: 900, height: 1180 },
							storageState: authStateFile
						}
					}
				}
			},
			{
				test: {
					...shared,
					name: 'accessibility',
					include: ['spec/accessibility.spec.ts'],
					provide: {
						contextOptions: {
							...devices['Desktop Chrome'],
							storageState: { cookies: [], origins: [] }
						}
					}
				}
			},
			{
				test: {
					...shared,
					name: 'cli',
					include: ['spec/cli.spec.ts']
				}
			}
		]
	}
});
