import { defineConfig } from 'vite-plus';

export default defineConfig({
	fmt: {
		useTabs: true,
		singleQuote: true,
		trailingComma: 'none',
		printWidth: 100,
		sortPackageJson: true,
		ignorePatterns: [
			'.arcane.json',
			'.custom-gcl.yml',
			'.depot/**',
			'.devcontainer/**',
			'.github/**',
			'.vscode/**',
			'backend/**',
			'cli/**',
			'docker/**',
			'types/**',
			'AI_POLICY.md',
			'CHANGELOG.md',
			'CONTRIBUTING.md',
			'SECURITY.md',
			'cliff.toml',
			'depot.json',
			'pnpm-lock.yaml',
			'frontend/.svelte-kit/**',
			'frontend/build/**',
			'frontend/messages/**',
			'frontend/project.inlang/**',
			'frontend/src/lib/paraglide/**',
			'tests/.auth/**',
			'tests/.bin/**',
			'tests/.report/**',
			'tests/test-results/**'
		],
		overrides: [
			{
				files: ['frontend/**'],
				options: {
					printWidth: 130,
					sortPackageJson: false,
					svelte: true,
					sortTailwindcss: {
						stylesheet: './frontend/src/routes/layout.css',
						attributes: ['class'],
						functions: ['clsx', 'cn'],
						preserveWhitespace: true
					}
				}
			}
		]
	},
	staged: {
		'frontend/**/*': "sh -c 'just format frontend --check'",
		'{tests,email-templates}/**/*.{ts,tsx,js,jsx,mts,cts}': "sh -c 'just format js --check'",
		'{backend,cli,types}/**/*': "sh -c 'just format go --check'"
	},
	test: {
		exclude: ['**/node_modules/**', 'tests/**'],
		passWithNoTests: true
	},
	lint: {
		jsPlugins: [{ name: 'vite-plus', specifier: 'vite-plus/oxlint-plugin' }],
		rules: { 'vite-plus/prefer-vite-plus-imports': 'error' },
		options: { typeAware: true, typeCheck: false }
	}
});
