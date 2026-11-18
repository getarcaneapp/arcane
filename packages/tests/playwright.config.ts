import { defineConfig, devices } from '@playwright/test';
import path from 'node:path';
import { authStateFile, reportDirectory } from './utils/report';

const baseURL = process.env.BASE_URL || 'http://localhost:3000';
const ciJunitOutputFile = path.join(
	reportDirectory,
	path.basename(process.env.PLAYWRIGHT_JUNIT_OUTPUT_FILE || 'junit.xml')
);
const configuredWorkers = process.env.PLAYWRIGHT_WORKERS;
const workers =
	configuredWorkers && Number.isInteger(Number(configuredWorkers))
		? Number(configuredWorkers)
		: configuredWorkers || 1;

export default defineConfig({
	testDir: '.',
	outputDir: path.join(reportDirectory, 'results'),
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	failOnFlakyTests: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	retryStrategy: 'immediate',
	globalTimeout: process.env.CI ? 35 * 60 * 1000 : 0,
	maxFailures: process.env.CI ? 20 : 0,
	workers,
	globalSetup: ['./setup/docker-browser', './setup/global-setup'],
	globalTeardown: './setup/global-teardown',
	reporter: process.env.CI
		? [
				['./utils/compose-reporter.ts'],
				['html', { outputFolder: path.join(reportDirectory, 'html') }],
				['github'],
				[
					'junit',
					{
						outputFile: ciJunitOutputFile,
						includeProjectInTestName: true,
						stripANSIControlSequences: true
					}
				]
			]
		: [
				['./utils/compose-reporter.ts'],
				['line'],
				['html', { open: 'never', outputFolder: path.join(reportDirectory, 'html') }]
			],
	use: {
		baseURL,
		serviceWorkers: 'block',
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		video: 'retain-on-failure'
	},
	projects: [
		{
			name: 'auth-setup',
			testMatch: '**/setup/auth.setup.ts'
		},
		{
			name: 'gitops-setup',
			testMatch: '**/setup/gitops.setup.ts',
			use: { storageState: authStateFile },
			dependencies: ['auth-setup']
		},
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'], storageState: authStateFile },
			dependencies: ['auth-setup', 'gitops-setup'],
			testMatch: '**/spec/*.spec.ts',
			testIgnore: [
				'**/spec/cli.spec.ts',
				'**/spec/responsive.spec.ts',
				'**/spec/accessibility.spec.ts'
			]
		},
		{
			name: 'mobile-chromium',
			use: { ...devices['Pixel 7'], storageState: authStateFile },
			dependencies: ['auth-setup'],
			testMatch: '**/spec/responsive.spec.ts'
		},
		{
			name: 'tablet-chromium',
			use: {
				...devices['Desktop Chrome'],
				viewport: { width: 900, height: 1180 },
				storageState: authStateFile
			},
			dependencies: ['auth-setup'],
			testMatch: '**/spec/responsive.spec.ts'
		},
		{
			name: 'accessibility',
			use: {
				...devices['Desktop Chrome'],
				storageState: { cookies: [], origins: [] }
			},
			dependencies: ['auth-setup'],
			testMatch: '**/spec/accessibility.spec.ts'
		},
		{
			name: 'cli',
			testMatch: '**/spec/cli.spec.ts'
		}
	]
});
