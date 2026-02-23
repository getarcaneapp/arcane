import { type Completion, snippetCompletion } from '@codemirror/autocomplete';
import type { DiagnosticSummary } from './analysis/types';

export const YAML_SNIPPETS: Completion[] = [
	snippetCompletion('services:\n  ${1:service}:\n    image: ${2:image:tag}', {
		label: 'service',
		type: 'snippet',
		detail: 'Service skeleton'
	}),
	snippetCompletion(
		'healthcheck:\n  test: ["CMD", "${1:command}"]\n  interval: ${2:30s}\n  timeout: ${3:10s}\n  retries: ${4:3}',
		{
			label: 'healthcheck',
			type: 'snippet',
			detail: 'Healthcheck block'
		}
	),
	snippetCompletion('ports:\n  - "${1:8080}:${2:80}"', { label: 'ports', type: 'snippet' }),
	snippetCompletion('volumes:\n  - ${1:source}:${2:/path}', { label: 'volumes', type: 'snippet' }),
	snippetCompletion('depends_on:\n  ${1:service}:\n    condition: ${2:service_healthy}', {
		label: 'depends_on',
		type: 'snippet'
	}),
	snippetCompletion('restart: ${1:unless-stopped}', { label: 'restart', type: 'snippet' }),
	snippetCompletion('build:\n  context: ${1:.}\n  dockerfile: ${2:Dockerfile}', { label: 'build', type: 'snippet' })
];

export const ENV_SNIPPETS: Completion[] = [
	snippetCompletion('${1:KEY}=${2:value}', { label: 'KEY=value', type: 'snippet' }),
	snippetCompletion('# ${1:Comment}', { label: 'comment', type: 'snippet' })
];

export function createDefaultSummary(): DiagnosticSummary {
	return {
		errors: 0,
		warnings: 0,
		infos: 0,
		hints: 0,
		schemaStatus: 'unavailable',
		schemaMessage: undefined,
		unresolvedVariables: [],
		secretWarnings: 0,
		duplicateKeyWarnings: 0,
		duplicateEnvWarnings: 0,
		cursorLine: 1,
		cursorCol: 1,
		validationReady: false
	};
}
