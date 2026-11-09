<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { composeTreeSplitProps } from '#lib/utils/compose-flow.js';
	import { workspaceFileBasename } from '#lib/utils/workspace-files.js';

	import type { BuildWorkspaceDraft } from '../build-workspace-draft.svelte';
	import BuildWorkspaceFile from './build-workspace-file.svelte';
	import BuildWorkspaceTree from './build-workspace-tree.svelte';

	let {
		workspace,
		rootLabel,
		loading,
		error,
		truncated = false,
		canEdit,
		maxFileSizeMb,
		onRetry
	}: {
		workspace: BuildWorkspaceDraft;
		rootLabel: string;
		loading: boolean;
		error: Error | null;
		truncated?: boolean;
		canEdit: boolean;
		maxFileSizeMb: number;
		onRetry: () => void;
	} = $props();

	const tabs = $derived(
		workspace.fileTabs.map((key) => {
			const relativePath = key.slice(5);
			return {
				key,
				label: workspaceFileBasename(relativePath),
				title: relativePath,
				iconClass: 'text-muted-foreground',
				pending:
					workspace.changedPaths.includes(relativePath) ||
					workspace.entries.find((file) => file.relativePath === relativePath)?.pending === true
			};
		})
	);

	let treeWidth = $state<number | null>(null);
</script>

<ResizableSplit
	class="min-h-0 flex-1"
	{...composeTreeSplitProps}
	bind:size={treeWidth}
	ariaLabel={m.compose_editor_resize_files_panel()}
	persistKey="arcane.build.workspace.split"
>
	{#snippet first()}
		<BuildWorkspaceTree {workspace} {rootLabel} {loading} {error} {truncated} {canEdit} {onRetry} />
	{/snippet}

	{#snippet second()}
		<EditorTabStrip {tabs} activeKey={workspace.activeTab} onSelect={workspace.openTab} onClose={workspace.closeTab}>
			{#snippet actions()}
				{#if workspace.hasChanges && canEdit}
					<ArcaneButton
						action="save"
						size="icon"
						class="size-7"
						showLabel={false}
						customLabel={m.common_save()}
						title={m.common_save()}
						loading={workspace.saving}
						disabled={workspace.saving}
						onclick={workspace.save}
					/>
				{/if}
			{/snippet}
			{#if workspace.activeTab}
				{#key workspace.activeTab}
					<BuildWorkspaceFile {workspace} relativePath={workspace.activeTab.slice(5)} readOnly={!canEdit} {maxFileSizeMb} />
				{/key}
			{:else}
				<EmptyState class="flex-1" icon={FileTextIcon} title={m.no_file_open()} description={m.volumes_workspace_select_file()} />
			{/if}
		</EditorTabStrip>
	{/snippet}
</ResizableSplit>
