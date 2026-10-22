<script lang="ts" module>
	import { WorkspaceDraftState } from '#lib/components/workspace-editor/workspace-draft-state.svelte.js';
	import type { PodmanQuadletFile } from '#lib/types/podman.js';
	import { planWorkspaceFileCreate, planWorkspaceFileMove, planWorkspaceFileRename } from '#lib/utils/workspace-files.js';

	export function createQuadletWorkspace(files: PodmanQuadletFile[] = []): WorkspaceDraftState {
		return new WorkspaceDraftState({
			initialFiles: files.map((file) => ({ relativePath: file.name, isDirectory: false })),
			initialContents: Object.fromEntries(files.map((file) => [file.name, file.content])),
			initialOpenTabs: ['main'],
			initialSelection: 'main',
			fallbackTab: 'main',
			pendingEntries: false,
			allowBinary: false,
			isFixedTab: (key) => key === 'main',
			planCreate: planWorkspaceFileCreate,
			planRename: planWorkspaceFileRename,
			planMove: planWorkspaceFileMove
		});
	}

	export function quadletWorkspaceFiles(workspace: WorkspaceDraftState): PodmanQuadletFile[] {
		return workspace.files
			.filter((file) => !file.isDirectory)
			.map((file) => ({ name: file.relativePath, content: workspace.contents[file.relativePath] ?? '' }));
	}
</script>

<script lang="ts">
	import CodePanel from '#lib/components/code-panel.svelte';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import { useEnvironmentSettings } from '#lib/hooks/use-environment-settings.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';
	import { composeTreeSplitProps } from '#lib/utils/compose-flow.js';
	import { validateWorkspaceFileName, workspaceFileBasename, workspaceFileLanguage } from '#lib/utils/workspace-files.js';

	let {
		workspace,
		mainFileName,
		content = $bindable(''),
		fileId,
		environmentId,
		disabled = false,
		readOnly = false,
		class: className
	}: {
		workspace: WorkspaceDraftState;
		mainFileName: string;
		content?: string;
		fileId: string;
		environmentId: string;
		disabled?: boolean;
		readOnly?: boolean;
		class?: string;
	} = $props();

	const environmentSettings = useEnvironmentSettings(() => environmentId);
	const maxFileSizeMb = $derived(environmentSettings.current?.values.projectWorkspaceMaxFileSizeMb ?? 10);
	const editable = $derived(!disabled && !readOnly);
	const activeTab = $derived(workspace.activeTab);
	const leadingRows = $derived([{ key: 'main', label: mainFileName, iconClass: 'text-info', locked: true }]);
	const tabs = $derived(
		workspace.validOpenTabs.map((key) => ({
			key,
			label: key === 'main' ? mainFileName : workspaceFileBasename(key.slice(5)),
			title: key === 'main' ? mainFileName : key.slice(5),
			iconClass: key === 'main' ? 'text-info' : 'text-muted-foreground',
			pending: false
		}))
	);

	let treeWidth = $state<number | null>(null);
</script>

<div class={cn('flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-card', className)}>
	<ResizableSplit
		class="min-h-0 flex-1"
		{...composeTreeSplitProps}
		bind:size={treeWidth}
		ariaLabel={m.compose_editor_resize_files_panel()}
		persistKey="arcane.podman.quadlet.split"
	>
		{#snippet first()}
			<WorkspaceFileTreePanel
				{leadingRows}
				entries={workspace.entries}
				selectedFile={workspace.selectedKey}
				disabled={!editable}
				onSelect={workspace.openTab}
				onCreateFile={editable ? workspace.createFile : undefined}
				onUpload={editable ? (parentPath, files) => workspace.uploadFile(parentPath, files, maxFileSizeMb) : undefined}
				validateName={(name) => validateWorkspaceFileName(name)}
				onRename={editable ? workspace.rename : undefined}
				onDelete={editable ? workspace.remove : undefined}
			/>
		{/snippet}

		{#snippet second()}
			<div class="flex h-full min-h-0 flex-1 flex-col">
				<EditorTabStrip {tabs} activeKey={activeTab} onSelect={workspace.openTab} onClose={workspace.closeTab} />
				<div class="flex min-h-0 flex-1 flex-col">
					{#key activeTab}
						{#if activeTab === 'main'}
							<CodePanel
								variant="plain"
								open={true}
								title={mainFileName}
								language="systemd"
								validationMode="none"
								bind:value={content}
								readOnly={!editable}
								fileId={`${fileId}:main`}
							/>
						{:else if activeTab.startsWith('file:')}
							{@const relativePath = activeTab.slice(5)}
							<CodePanel
								variant="plain"
								open={true}
								title={relativePath}
								language={workspaceFileLanguage(relativePath)}
								validationMode="none"
								bind:value={workspace.contents[relativePath]}
								readOnly={!editable}
								fileId={`${fileId}:file:${relativePath}`}
							/>
						{/if}
					{/key}
				</div>
			</div>
		{/snippet}
	</ResizableSplit>
</div>
