import type { GitOpsSync } from '#lib/types/automation.js';
import { toGitRouteUrl, toGitWebUrl } from '#lib/utils/navigation.js';

function composeDirectory(sync: GitOpsSync): string {
	return sync.composePath.split('/').slice(0, -1).join('/');
}

// Mirrors the backend's workspace lock, which deliberately omits the project-root .env.
export function syncedPaths(sync: GitOpsSync): string[] {
	try {
		const parsed: unknown = JSON.parse(sync.syncedFiles ?? '[]');
		return Array.isArray(parsed) ? parsed.filter((entry) => typeof entry === 'string' && entry) : [];
	} catch {
		return [];
	}
}

// Unknown forges still get a link to the repository itself.
export function gitOpsProjectUrl(sync: GitOpsSync | undefined | null): string | null {
	if (!sync?.repository?.url) return null;
	return toGitRouteUrl(sync.repository.url, 'tree', sync.branch, composeDirectory(sync)) ?? toGitWebUrl(sync.repository.url);
}

export function gitOpsComposeEditUrl(sync: GitOpsSync | undefined | null): string | null {
	if (!sync?.repository?.url) return null;
	return toGitRouteUrl(sync.repository.url, 'edit', sync.branch, sync.composePath);
}

export function gitOpsFileEditUrl(sync: GitOpsSync | undefined | null, relativePath: string): string | null {
	if (!sync?.repository?.url || !syncedPaths(sync).includes(relativePath)) return null;
	const directory = composeDirectory(sync);
	return toGitRouteUrl(sync.repository.url, 'edit', sync.branch, directory ? `${directory}/${relativePath}` : relativePath);
}
