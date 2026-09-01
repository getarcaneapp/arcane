import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.BASE_URL || 'http://localhost:3000';
const ciJunitOutputFile = process.env.PLAYWRIGHT_JUNIT_OUTPUT_FILE || 'test-results/junit.xml';
const configuredWorkers = process.env.PLAYWRIGHT_WORKERS;
const workers =
	configuredWorkers && Number.isInteger(Number(configuredWorkers))
		? Number(configuredWorkers)
		: configuredWorkers || (process.env.CI ? 1 : 2);

export default defineConfig({
	testDir: '.',
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	failOnFlakyTests: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	retryStrategy: 'isolated',
	workers,
	globalSetup: './setup/global-setup',
	globalTeardown: './setup/global-teardown',
	reporter: process.env.CI
		? [
				['html', { outputFolder: '.report' }],
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
		: [['line'], ['html', { open: 'never', outputFolder: '.report' }]],
	use: {
		baseURL,
		serviceWorkers: 'block',
		trace: 'on-first-retry',
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
			use: { storageState: '.auth/login.json' },
			dependencies: ['auth-setup']
		},
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'], storageState: '.auth/login.json' },
			dependencies: ['auth-setup', 'gitops-setup'],
			testMatch: '**/spec/*.spec.ts',
			testIgnore: [
				'**/spec/cli.spec.ts',
				'**/spec/responsive-browser.spec.ts',
				'**/spec/accessibility-check.spec.ts'
			]
		},
		{
			name: 'mobile-chromium',
			use: { ...devices['Pixel 7'], storageState: '.auth/login.json' },
			dependencies: ['auth-setup'],
			testMatch: '**/spec/responsive-browser.spec.ts'
		},
		{
			name: 'tablet-chromium',
			use: {
				...devices['Desktop Chrome'],
				viewport: { width: 900, height: 1180 },
				storageState: '.auth/login.json'
			},
			dependencies: ['auth-setup'],
			testMatch: '**/spec/responsive-browser.spec.ts'
		},
		{
			name: 'firefox',
			use: {
				...devices['Desktop Firefox'],
				storageState: { cookies: [], origins: [] }
			},
			dependencies: ['auth-setup'],
			testMatch: ['**/spec/token-refresh.spec.ts', '**/spec/network.spec.ts'],
			grep: /@cross-browser/
		},
		{
			name: 'accessibility',
			use: {
				...devices['Desktop Chrome'],
				storageState: { cookies: [], origins: [] }
			},
			dependencies: ['auth-setup'],
			testMatch: '**/spec/accessibility-check.spec.ts'
		},
		{
			name: 'cli',
			testMatch: '**/spec/cli.spec.ts'
		}
	]
});
