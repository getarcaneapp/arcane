<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FileTreeRow from '#lib/components/file-tree-row.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		compareWorkspaceFileEntries,
		workspaceFileParentPath,
		type WorkspaceDisplayEntry
	} from '#lib/utils/workspace-files.js';

	import type { BuildWorkspaceDraft } from '../build-workspace-draft.svelte';

	let {
		workspace,
		buildsRoot,
		disabled = false
	}: {
		workspace: BuildWorkspaceDraft;
		buildsRoot: string;
		disabled?: boolean;
	} = $props();

	let open = $state(false);
	let selectedPath = $state('');
	let openFolders = $state<Record<string, boolean>>({});

	const rows = $derived.by(() => {
		const byParent = Map.groupBy(
			workspace.entries.filter((entry) => entry.isDirectory).toSorted(compareWorkspaceFileEntries),
			(entry) => workspaceFileParentPath(entry.relativePath)
		);
		const result: (WorkspaceDisplayEntry & { depth: number })[] = [];
		const append = (parentPath: string, depth: number) => {
			for (const entry of byParent.get(parentPath) ?? []) {
				result.push({ ...entry, depth });
				if (openFolders[entry.relativePath] === true) append(entry.relativePath, depth + 1);
			}
		};
		append('', 1);
		return result;
	});

	function show() {
		const expanded: Record<string, boolean> = {};
		for (let folder = workspaceFileParentPath(workspace.contextPath); folder; folder = workspaceFileParentPath(folder)) {
			expanded[folder] = true;
		}
		openFolders = expanded;
		selectedPath = workspace.contextPath;
		open = true;
	}

	function confirm() {
		workspace.contextPath = selectedPath;
		open = false;
	}
</script>

{#snippet contextBadge(relativePath: string)}
	{#if relativePath === workspace.contextPath}
		<Badge variant="purple" size="xs">{m.context()}</Badge>
	{/if}
{/snippet}

<ArcaneButton action="base" tone="outline" icon={FolderOpenIcon} customLabel={m.choose_folder()} {disabled} onclick={show} />

<Dialog.Root bind:open>
	<Dialog.Content class="max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{m.choose_folder()}</Dialog.Title>
			<Dialog.Description>{m.build_choose_context_description()}</Dialog.Description>
		</Dialog.Header>
		<div class="max-h-screen-60 min-h-64 overflow-auto rounded-xl bg-card/10 p-1">
			<FileTreeRow
				name={buildsRoot}
				path=""
				depth={0}
				isDirectory={true}
				showDisclosure={false}
				selected={selectedPath === ''}
				expandLabel={m.workspace_file_expand_folder({ name: buildsRoot })}
				collapseLabel={m.workspace_file_collapse_folder({ name: buildsRoot })}
				onActivate={() => (selectedPath = '')}
			>
				{#snippet trailing()}
					{@render contextBadge('')}
				{/snippet}
			</FileTreeRow>
			{#each rows as row (row.relativePath)}
				<FileTreeRow
					name={row.name}
					path={row.relativePath}
					depth={row.depth}
					isDirectory={true}
					expanded={openFolders[row.relativePath] === true}
					selected={selectedPath === row.relativePath}
					pending={row.pending}
					pendingLabel={m.common_unsaved_changes()}
					expandLabel={m.workspace_file_expand_folder({ name: row.name })}
					collapseLabel={m.workspace_file_collapse_folder({ name: row.name })}
					onToggle={() => (openFolders = { ...openFolders, [row.relativePath]: openFolders[row.relativePath] !== true })}
					onActivate={() => (selectedPath = row.relativePath)}
				>
					{#snippet trailing()}
						{@render contextBadge(row.relativePath)}
					{/snippet}
				</FileTreeRow>
			{/each}
		</div>
		<Dialog.Footer>
			<ArcaneButton action="cancel" onclick={() => (open = false)} />
			<ArcaneButton action="confirm" customLabel={m.use_as_build_context()} onclick={confirm} />
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
