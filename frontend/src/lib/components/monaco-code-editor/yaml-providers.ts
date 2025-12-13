import type * as Monaco from 'monaco-editor';

/**
 * Docker Compose schema definitions for auto-completion and validation
 */
const composeSchema = {
	version: ['3.8', '3.9', '3'],
	services: {
		image: 'Image name (e.g., nginx:latest)',
		container_name: 'Custom container name',
		ports: 'Port mappings (e.g., "8080:80")',
		volumes: 'Volume mounts',
		environment: 'Environment variables',
		networks: 'Networks to attach',
		depends_on: 'Service dependencies',
		command: 'Override default command',
		entrypoint: 'Override entrypoint',
		restart: ['no', 'always', 'on-failure', 'unless-stopped'],
		build: 'Build configuration',
		labels: 'Container labels',
		healthcheck: 'Health check configuration',
		deploy: 'Deploy configuration (Swarm mode)',
		configs: 'Configuration references',
		secrets: 'Secret references'
	},
	networks: 'Network definitions',
	volumes: 'Volume definitions',
	configs: 'Config definitions',
	secrets: 'Secret definitions'
};

/**
 * Register YAML completion provider for Docker Compose files
 */
export function registerYamlCompletionProvider(monaco: typeof Monaco) {
	return monaco.languages.registerCompletionItemProvider('yaml', {
		triggerCharacters: [' ', ':'],
		provideCompletionItems: (model, position) => {
			const lineContent = model.getLineContent(position.lineNumber);
			const lineUntilPosition = lineContent.substring(0, position.column - 1);
			const word = model.getWordUntilPosition(position);

			const range = {
				startLineNumber: position.lineNumber,
				endLineNumber: position.lineNumber,
				startColumn: word.startColumn,
				endColumn: word.endColumn
			};

			const suggestions: Monaco.languages.CompletionItem[] = [];

			// Root level suggestions
			if (!lineUntilPosition.trim() || lineUntilPosition.match(/^\s*$/)) {
				suggestions.push(
					{
						label: 'version',
						kind: monaco.languages.CompletionItemKind.Property,
						documentation: 'Docker Compose file version',
						insertText: 'version: "3.8"',
						range
					},
					{
						label: 'services',
						kind: monaco.languages.CompletionItemKind.Property,
						documentation: 'Service definitions',
						insertText: 'services:\n  ${1:service_name}:\n    image: ${2:image_name}',
						insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
						range
					},
					{
						label: 'networks',
						kind: monaco.languages.CompletionItemKind.Property,
						documentation: 'Network definitions',
						insertText: 'networks:\n  ${1:network_name}:\n    driver: ${2:bridge}',
						insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
						range
					},
					{
						label: 'volumes',
						kind: monaco.languages.CompletionItemKind.Property,
						documentation: 'Volume definitions',
						insertText: 'volumes:\n  ${1:volume_name}:',
						insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
						range
					}
				);
			}

			// Service property suggestions
			if (lineUntilPosition.match(/^\s{2,}\w*$/) && model.getValue().includes('services:')) {
				Object.entries(composeSchema.services).forEach(([key, value]) => {
					const isArray = Array.isArray(value);
					suggestions.push({
						label: key,
						kind: monaco.languages.CompletionItemKind.Property,
						documentation: isArray ? `Options: ${value.join(', ')}` : value,
						insertText: isArray ? `${key}: ${value[0]}` : `${key}: `,
						range
					});
				});
			}

			return { suggestions };
		}
	});
}

/**
 * Register YAML hover provider for documentation
 */
export function registerYamlHoverProvider(monaco: typeof Monaco) {
	return monaco.languages.registerHoverProvider('yaml', {
		provideHover: (model, position) => {
			const word = model.getWordAtPosition(position);
			if (!word) return null;

			const docs: Record<string, string> = {
				version: '**Docker Compose version**\n\nRecommended: `3.8` or `3.9`',
				services: '**Services**\n\nDefine containers that make up your application',
				image: '**Image**\n\nDocker image to use (e.g., `nginx:latest`, `postgres:13`)',
				container_name: '**Container Name**\n\nCustom name for the container',
				ports: '**Ports**\n\nPort mappings between host and container\n\nFormat: `"HOST:CONTAINER"`',
				volumes: '**Volumes**\n\nBind mounts or named volumes\n\nFormat: `"./local:/container"`',
				environment: '**Environment Variables**\n\nSet environment variables in the container',
				networks: '**Networks**\n\nNetworks to attach this service to',
				depends_on: '**Dependencies**\n\nServices that must start before this one',
				restart: '**Restart Policy**\n\nOptions: `no`, `always`, `on-failure`, `unless-stopped`',
				build: '**Build**\n\nBuild configuration for custom images',
				command: '**Command**\n\nOverride the default command',
				healthcheck: '**Health Check**\n\nConfigure container health monitoring'
			};

			const documentation = docs[word.word];
			if (documentation) {
				return {
					range: new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn),
					contents: [{ value: documentation }]
				};
			}

			return null;
		}
	});
}

/**
 * Register all YAML providers for Docker Compose editing
 */
export function registerYamlProviders(monaco: typeof Monaco) {
	return [registerYamlCompletionProvider(monaco), registerYamlHoverProvider(monaco)];
}
