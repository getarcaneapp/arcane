import { defineConfig } from 'vite-plus';

export default defineConfig({
	fmt: {
		useTabs: true,
		singleQuote: true,
		trailingComma: 'none',
		printWidth: 100,
		sortPackageJson: true,
		ignorePatterns: [
			'backend/**',
			'cli/**',
			'types/**',
			'.devcontainer/devcontainer-lock.json',
			'frontend/.svelte-kit/**',
			'frontend/build/**',
			'frontend/messages/**',
			'frontend/src/lib/paraglide/**',
			'tests/.report/**'
		],
		overrides: [
			{
				files: ['frontend/**'],
				options: {
					printWidth: 130,
					sortImports: true,
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
		'tests/**/*.{ts,tsx,js,jsx,mts,cts}': "sh -c 'just format tests --check'",
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
