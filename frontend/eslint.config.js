import { plugin as shadcn } from '@shadcn/lint';
import tsParser from '@typescript-eslint/parser';
import { defineConfig } from 'eslint/config';
import svelteParser from 'svelte-eslint-parser';

const shadcnRules = {
	'shadcn/no-restyle': ['error', { allow: ['layout'] }],
	'shadcn/no-raw-colors': 'error',
	'shadcn/no-arbitrary-values': 'error',
	'shadcn/no-inline-styles': 'error',
	'shadcn/no-unknown-classes': 'error',
	'shadcn/require-static-classes': 'error'
};

export default defineConfig([
	{
		ignores: ['.svelte-kit/**', 'build/**', 'src/paraglide/**', 'src/lib/paraglide/**']
	},
	{
		files: ['**/*.svelte'],
		languageOptions: {
			parser: svelteParser,
			parserOptions: { parser: tsParser }
		},
		plugins: { shadcn },
		rules: shadcnRules
	},
	{
		files: ['**/*.ts'],
		languageOptions: { parser: tsParser },
		plugins: { shadcn },
		rules: shadcnRules
	},
	{
		// Components own their styles.
		files: ['src/lib/components/ui/**'],
		rules: {
			'shadcn/no-restyle': 'off',
			'shadcn/no-arbitrary-values': 'off',
			'shadcn/require-static-classes': 'off'
		}
	}
]);
