import type { DiagnosticSummary, SnippetItem } from './analysis/types';

export const YAML_SNIPPETS: SnippetItem[] = [
	{
		label: 'service',
		type: 'snippet',
		detail: 'Service skeleton',
		insert: 'services:\n  ${1:service}:\n    image: ${2:image:tag}'
	},
	{
		label: 'healthcheck',
		type: 'snippet',
		detail: 'Healthcheck block',
		insert: 'healthcheck:\n  test: ["CMD", "${1:command}"]\n  interval: ${2:30s}\n  timeout: ${3:10s}\n  retries: ${4:3}'
	},
	{ label: 'ports', type: 'snippet', insert: 'ports:\n  - "${1:8080}:${2:80}"' },
	{ label: 'volumes', type: 'snippet', insert: 'volumes:\n  - ${1:source}:${2:/path}' },
	{ label: 'depends_on', type: 'snippet', insert: 'depends_on:\n  ${1:service}:\n    condition: ${2:service_healthy}' },
	{ label: 'restart', type: 'snippet', insert: 'restart: ${1:unless-stopped}' },
	{ label: 'build', type: 'snippet', insert: 'build:\n  context: ${1:.}\n  dockerfile: ${2:Dockerfile}' }
];

export const ENV_SNIPPETS: SnippetItem[] = [
	{ label: 'KEY=value', type: 'snippet', insert: '${1:KEY}=${2:value}' },
	{ label: 'comment', type: 'snippet', insert: '# ${1:Comment}' }
];

const SNIPPET_PLACEHOLDER_REGEX = /\$\{\d+:?([^}]*)\}/g;

/**
 * Resolve a snippet template into plain text plus the range of the first
 * placeholder (relative to the resolved text) so the caret can select it.
 */
export function resolveSnippet(template: string): { text: string; selectFrom?: number; selectTo?: number } {
	let selectFrom: number | undefined;
	let selectTo: number | undefined;
	let result = '';
	let last = 0;

	for (const match of template.matchAll(SNIPPET_PLACEHOLDER_REGEX)) {
		const index = match.index ?? 0;
		result += template.slice(last, index);
		const placeholder = match[1] ?? '';
		if (selectFrom === undefined) {
			selectFrom = result.length;
			selectTo = result.length + placeholder.length;
		}
		result += placeholder;
		last = index + match[0].length;
	}
	result += template.slice(last);

	return { text: result, selectFrom, selectTo };
}

export function createDefaultSummary(): DiagnosticSummary {
	return {
		errors: 0,
		warnings: 0,
		infos: 0,
		hints: 0,
		schemaStatus: 'unavailable',
		schemaMessage: undefined,
		cursorLine: 1,
		cursorCol: 1,
		validationReady: false
	};
}
