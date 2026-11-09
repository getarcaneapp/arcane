<script lang="ts">
	import { resolve } from '$app/paths';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { EmptyState } from '#lib/components/states/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import { AlertIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { validateWorkspaceFileName, type WorkspaceDisplayEntry } from '#lib/utils/workspace-files.js';

	import type { BuildWorkspaceDraft } from '../build-workspace-draft.svelte';

	let {
		workspace,
		rootLabel,
		loading,
		error,
		truncated = false,
		canEdit,
		onRetry
	}: {
		workspace: BuildWorkspaceDraft;
		rootLabel: string;
		loading: boolean;
		error: Error | null;
		truncated?: boolean;
		canEdit: boolean;
		onRetry: () => void;
	} = $props();

	const disabled = $derived(!canEdit || workspace.saving);
	const canOpenBuildSettings = $derived(hasPermission('environment-settings:read', workspace.environmentId));
	const rootMenuItems = $derived(
		workspace.contextPath
			? [
					{
						id: 'build-context',
						label: m.use_as_build_context(),
						icon: FolderOpenIcon,
						onSelect: () => (workspace.contextPath = '')
					}
				]
			: []
	);

	function rowMenuItems(entry: WorkspaceDisplayEntry) {
		if (!entry.isDirectory) return [];
		return [
			{
				id: 'build-context',
				label: m.use_as_build_context(),
				icon: FolderOpenIcon,
				disabled: entry.relativePath === workspace.contextPath,
				onSelect: () => (workspace.contextPath = entry.relativePath)
			}
		];
	}
</script>

{#snippet contextBadge(entry: WorkspaceDisplayEntry)}
	{#if entry.isDirectory && entry.relativePath === workspace.contextPath}
		<Badge variant="purple" size="xs">{m.context()}</Badge>
	{/if}
{/snippet}

{#snippet standaloneHeader()}
	<div class="flex h-10 shrink-0 items-center px-2">
		<span class="truncate text-xs font-medium text-muted-foreground">{rootLabel}</span>
	</div>
{/snippet}

{#if loading}
	<div class="flex min-h-0 flex-1 flex-col">
		{@render standaloneHeader()}
		<div class="flex flex-1 items-center justify-center p-6">
			<Spinner tone="muted" class="size-6" />
		</div>
	</div>
{:else if error}
	<div class="flex min-h-0 flex-1 flex-col overflow-auto">
		{@render standaloneHeader()}
		<EmptyState
			icon={AlertIcon}
			title={m.build_workspace_unavailable_title()}
			description={m.build_workspace_unavailable_description()}
		>
			<p class="max-w-md text-xs break-words text-muted-foreground">{extractApiErrorMessage(error)}</p>
			<div class="flex flex-wrap justify-center gap-2">
				<ArcaneButton action="refresh" customLabel={m.common_retry()} onclick={onRetry} />
				{#if canOpenBuildSettings}
					<ArcaneButton
						action="base"
						tone="outline"
						customLabel={m.build_workspace_open_settings()}
						href={resolve(`/(app)/settings/builds?env=${encodeURIComponent(workspace.environmentId)}`)}
					/>
				{:else}
					<p class="text-xs text-muted-foreground">{m.build_workspace_ask_admin()}</p>
				{/if}
			</div>
		</EmptyState>
	</div>
{:else}
	<WorkspaceFileTreePanel
		title={rootLabel}
		{rootMenuItems}
		{rowMenuItems}
		rowBadge={contextBadge}
		entries={workspace.entries}
		selectedFile={workspace.activeTab}
		{disabled}
		multipleUploads={true}
		uploadDescription={m.build_workspace_upload_description()}
		validateName={validateWorkspaceFileName}
		onSelect={workspace.openTab}
		onCreateFile={canEdit ? workspace.createFile : undefined}
		onCreateFolder={canEdit ? workspace.createFolder : undefined}
		onUpload={canEdit ? workspace.upload : undefined}
		onRename={canEdit ? workspace.rename : undefined}
		onMove={canEdit ? workspace.move : undefined}
		onDelete={canEdit ? workspace.remove : undefined}
		onDownload={workspace.download}
		readOnlyMessage={truncated ? m.volumes_workspace_truncated() : undefined}
	/>
{/if}
