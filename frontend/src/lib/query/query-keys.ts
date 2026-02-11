import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

function stableSerialize(value: unknown): string {
	if (value === null || value === undefined) return 'null';
	if (typeof value !== 'object') return JSON.stringify(value);
	if (Array.isArray(value)) return `[${value.map((entry) => stableSerialize(entry)).join(',')}]`;

	const entries = Object.entries(value as Record<string, unknown>)
		.filter(([, entry]) => entry !== undefined)
		.sort(([a], [b]) => a.localeCompare(b));

	return `{${entries.map(([key, entry]) => `${JSON.stringify(key)}:${stableSerialize(entry)}`).join(',')}}`;
}

export const queryKeys = {
	auth: {
		all: ['auth'] as const,
		logout: () => ['auth', 'logout'] as const
	},
	settings: {
		all: ['settings'] as const,
		global: () => ['settings', 'global'] as const,
		byEnvironment: (environmentId: string) => ['settings', environmentId] as const
	},
	users: {
		all: ['users'] as const,
		list: (options: SearchPaginationSortRequest) => ['users', stableSerialize(options)] as const
	},
	apiKeys: {
		all: ['api-keys'] as const,
		list: (options: SearchPaginationSortRequest) => ['api-keys', stableSerialize(options)] as const
	},
	environments: {
		all: ['environments'] as const,
		list: (options: SearchPaginationSortRequest) => ['environments', stableSerialize(options)] as const,
		switcher: (options: SearchPaginationSortRequest) => ['environments', 'switcher', stableSerialize(options)] as const,
		detail: (environmentId: string) => ['environment', environmentId] as const,
		settings: (environmentId: string) => ['environment-settings', environmentId] as const,
		deploymentSnippets: (environmentId: string) => ['environment', 'deployment-snippets', environmentId] as const
	},
	gitRepositories: {
		all: ['git-repositories'] as const,
		list: (options: SearchPaginationSortRequest) => ['git-repositories', stableSerialize(options)] as const,
		syncDialog: () => ['git-repositories', 'sync-dialog'] as const,
		branches: (repositoryId: string) => ['git-repositories', 'branches', repositoryId] as const,
		files: (repositoryId: string, branch: string, path: string) => ['git-repository-files', repositoryId, branch, path] as const
	},
	containerRegistries: {
		all: ['container-registries'] as const,
		list: (options: SearchPaginationSortRequest) => ['container-registries', stableSerialize(options)] as const
	},
	templates: {
		all: ['templates'] as const,
		allTemplates: () => ['templates', 'all'] as const,
		defaults: () => ['templates', 'defaults'] as const,
		list: (options: SearchPaginationSortRequest) => ['templates', stableSerialize(options)] as const,
		content: (templateId: string) => ['template-content', templateId] as const,
		registries: () => ['template-registries'] as const,
		globalVariables: () => ['templates', 'global-variables'] as const
	},
	notifications: {
		settings: () => ['notification-settings'] as const
	},
	events: {
		all: ['events'] as const,
		listByEnvironment: (environmentId: string, options: SearchPaginationSortRequest) =>
			['events', environmentId, stableSerialize(options)] as const,
		listGlobal: (options: SearchPaginationSortRequest) => ['events', 'global', stableSerialize(options)] as const,
		deleteSelected: (environmentId: string) => ['events', 'delete-selected', environmentId] as const
	},
	system: {
		upgradeAvailable: (scope: 'mobile-nav' | 'sidebar') => ['system', 'upgrade-available', scope] as const,
		upgradeHealth: (environmentId: string) => ['system', 'upgrade-health', environmentId] as const,
		versionInfo: (environmentId: string) => ['system', 'version-info', environmentId] as const,
		dockerInfo: (environmentId: string) => ['system', 'docker-info', environmentId] as const
	},
	containers: {
		all: ['containers'] as const,
		list: (environmentId: string, options: SearchPaginationSortRequest) =>
			['containers', environmentId, stableSerialize(options)] as const,
		checkUpdates: (environmentId: string) => ['containers', 'check-updates', environmentId] as const,
		create: (environmentId: string) => ['containers', 'create', environmentId] as const,
		statusCounts: (environmentId: string) => ['containers', 'status-counts', environmentId] as const,
		detail: (environmentId: string, containerId: string) => ['container', environmentId, containerId] as const
	},
	images: {
		all: ['images'] as const,
		list: (environmentId: string, options: SearchPaginationSortRequest) =>
			['images', environmentId, stableSerialize(options)] as const,
		usageCounts: (environmentId: string) => ['images', 'usage-counts', environmentId] as const,
		detail: (environmentId: string, imageId: string) => ['image', environmentId, imageId] as const,
		updateCheck: (environmentId: string, imageId: string) => ['image-update', environmentId, imageId] as const
	},
	projects: {
		all: ['projects'] as const,
		list: (environmentId: string, options: SearchPaginationSortRequest) =>
			['projects', environmentId, stableSerialize(options)] as const,
		statusCounts: (environmentId: string) => ['projects', 'status-counts', environmentId] as const,
		detail: (environmentId: string, projectId: string) => ['project', environmentId, projectId] as const
	},
	networks: {
		all: ['networks'] as const,
		list: (environmentId: string, options: SearchPaginationSortRequest) =>
			['networks', environmentId, stableSerialize(options)] as const,
		detail: (environmentId: string, networkId: string) => ['network', environmentId, networkId] as const
	},
	gitOpsSyncs: {
		all: ['gitops-syncs'] as const,
		list: (environmentId: string, options: SearchPaginationSortRequest) =>
			['gitops-syncs', environmentId, stableSerialize(options)] as const
	},
	volumes: {
		table: (environmentId: string, options: SearchPaginationSortRequest) =>
			['volumes', environmentId, stableSerialize(options)] as const,
		detail: (environmentId: string, volumeName: string) => ['volume', environmentId, volumeName] as const,
		list: (volumeName: string, path: string) => ['volume-browser', volumeName, 'list', path] as const,
		listPrefix: (volumeName: string) => ['volume-browser', volumeName, 'list'] as const,
		content: (volumeName: string, path: string) => ['volume-browser', volumeName, 'content', path] as const,
		backups: (volumeName: string) => ['volume-backups', volumeName] as const,
		backupHasPath: (backupId: string, path: string) => ['volume-backups', backupId, 'has-path', path] as const
	},
	vulnerabilities: {
		summaryByEnvironment: (environmentId: string) => ['vulnerabilities', 'summary', environmentId] as const,
		summaryByImage: (imageId: string) => ['vulnerabilities', 'image-summary', imageId] as const,
		allByEnvironment: (environmentId: string, request: SearchPaginationSortRequest) =>
			['vulnerabilities', 'all', environmentId, stableSerialize(request)] as const,
		imageRows: (imageId: string, request: SearchPaginationSortRequest) =>
			['vulnerabilities', 'image', imageId, stableSerialize(request)] as const
	}
} as const;
