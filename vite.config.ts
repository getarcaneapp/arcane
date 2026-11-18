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
			'packages/tests/.report/**',
			'packages/ui/dist/**'
		],
		overrides: [
			{
				files: ['frontend/**', 'packages/ui/**'],
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
		'packages/tests/**/*.{ts,tsx,js,jsx,mts,cts}': "sh -c 'just format tests --check'",
		'packages/ui/**/*': "sh -c 'just format ui --check'",
		'{backend,cli,types}/**/*': "sh -c 'just format go --check'"
	},
	test: {
		exclude: ['**/node_modules/**', 'packages/tests/**'],
		passWithNoTests: true
	},
	lint: {
		ignorePatterns: ['backend/resources/scalar/**'],
		jsPlugins: [{ name: 'vite-plus', specifier: 'vite-plus/oxlint-plugin' }],
		rules: { 'vite-plus/prefer-vite-plus-imports': 'error' },
		options: { typeAware: true, typeCheck: false }
	}
});
