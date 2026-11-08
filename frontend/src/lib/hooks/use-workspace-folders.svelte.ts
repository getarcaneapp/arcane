import { createQueries, hashKey, useQueryClient, type CreateQueryOptions, type QueryKey } from '@tanstack/svelte-query';
import { tick } from 'svelte';
import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import type { Workspace, WorkspaceFileEntry } from '#lib/types/workspace.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import {
	applyWorkspaceFileChangesForDisplay,
	workspaceFileParentPath,
	workspaceFilePathMatches,
	workspaceFileServerPath,
	type WorkspaceDisplayFileChange
} from '#lib/utils/workspace-files.js';

type UseWorkspaceFoldersOptions<TQueryKey extends QueryKey> = {
	folderQuery: (path: string) => CreateQueryOptions<Workspace, Error, Workspace, TQueryKey>;
	enabled: () => boolean;
	// Staged changes shown in `displayFiles`; `load` and `loaded` then use displayed paths.
	changes?: () => WorkspaceDisplayFileChange[];
};

function workspaceFolderChain(relativePath: string): string[] {
	const folders: string[] = [];
	for (let folder = relativePath; folder; folder = workspaceFileParentPath(folder)) folders.unshift(folder);
	return folders;
}

export function useWorkspaceFolders<TQueryKey extends QueryKey>({
	folderQuery,
	enabled,
	changes
}: UseWorkspaceFoldersOptions<TQueryKey>) {
	const queryClient = useQueryClient();
	const scope = $derived(hashKey(folderQuery('').queryKey));
	let opened = $state({ scope: '', paths: [''] });
	const paths = $derived(opened.scope === scope ? opened.paths : ['']);
	const queries = createQueries(() => ({
		queries: paths.map((path) => ({ ...folderQuery(path), enabled: enabled() }))
	}));
	const files = $derived(queries.flatMap((query) => query.data?.files ?? []));
	const displayFiles = $derived(applyWorkspaceFileChangesForDisplay(files, changes?.() ?? []));
	const loadedPaths = $derived(new Set(paths.filter((_, index) => queries[index]?.data)));
	const loaded = $derived.by(() => {
		if (!changes) return loadedPaths;
		const staged = changes();
		return new Set(
			displayFiles
				.filter((file) => {
					if (!file.isDirectory) return false;
					const serverPath = workspaceFileServerPath(staged, file.relativePath);
					return serverPath === null || loadedPaths.has(serverPath);
				})
				.map((file) => file.relativePath)
		);
	});
	const truncated = $derived(queries.some((query) => query.data?.fileTreeTruncated));

	function setPaths(next: string[]) {
		opened = { scope, paths: [...new Set(next)] };
	}

	return {
		get root() {
			return queries[0];
		},
		get files() {
			return files;
		},
		get displayFiles() {
			return displayFiles;
		},
		get loaded() {
			return loaded;
		},
		get truncated() {
			return truncated;
		},
		reveal(filePaths: string[]) {
			setPaths([...paths, ...filePaths.flatMap((filePath) => workspaceFolderChain(workspaceFileParentPath(filePath)))]);
		},
		async load(relativePath: string): Promise<boolean> {
			if (!enabled()) return false;
			const serverPath = changes ? workspaceFileServerPath(changes(), relativePath) : relativePath;
			if (serverPath === null || (serverPath && !files.some((file) => file.relativePath === serverPath && file.isDirectory)))
				return true;
			const requestedScope = scope;
			setPaths([...paths, serverPath]);
			const result = await tryCatch(queryClient.ensureQueryData({ ...folderQuery(serverPath), revalidateIfStale: true }));
			if (requestedScope !== scope) return false;
			if (result.error !== null) {
				toast.error(m.common_load_failed({ resource: relativePath }), { description: extractApiErrorMessage(result.error) });
				return false;
			}
			await tick();
			return true;
		},
		keepSaved(entries: Pick<WorkspaceFileEntry, 'relativePath' | 'isDirectory'>[]) {
			setPaths([
				...paths.filter((path) => !path || entries.some((file) => file.relativePath === path && file.isDirectory)),
				...entries.map((file) => workspaceFileParentPath(file.relativePath))
			]);
		},
		forget(relativePath: string) {
			setPaths(paths.filter((path) => !workspaceFilePathMatches(path, relativePath)));
		}
	};
}
