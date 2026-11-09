<script lang="ts">
	import CodePanel from '#lib/components/code-panel.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { workspaceFileBasename, workspaceFileLanguage, workspaceReadOnlyMessage } from '#lib/utils/workspace-files.js';

	import type { BuildWorkspaceDraft } from '../build-workspace-draft.svelte';

	let {
		workspace,
		relativePath,
		readOnly,
		maxFileSizeMb
	}: {
		workspace: BuildWorkspaceDraft;
		relativePath: string;
		readOnly: boolean;
		maxFileSizeMb: number;
	} = $props();

	const metadata = $derived(workspace.metadata[relativePath]);
</script>

{#if workspace.loadErrors[relativePath]}
	<div class="flex h-full items-center justify-center px-4 text-sm text-destructive">
		{workspace.loadErrors[relativePath]}
	</div>
{:else if workspace.loading[relativePath] || !metadata}
	<div class="flex h-full items-center justify-center text-muted-foreground">
		<Spinner class="size-5" />
	</div>
{:else if metadata.editable && workspace.contents[relativePath] !== undefined}
	<CodePanel
		variant="plain"
		open={true}
		title={relativePath}
		language={workspaceFileLanguage(relativePath)}
		validationMode="none"
		bind:value={workspace.contents[relativePath]}
		{readOnly}
		fileId={`builds:${workspace.environmentId}:${relativePath}`}
		originalValue={workspace.loadedContents[relativePath] ?? ''}
		enableDiff={true}
	/>
{:else}
	<div class="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
		<FileTextIcon class="size-6 text-muted-foreground" />
		<p class="text-sm font-medium">{workspaceFileBasename(relativePath)}</p>
		<p class="max-w-md text-xs text-muted-foreground">
			{workspaceReadOnlyMessage(metadata.readOnlyReason, maxFileSizeMb)}
		</p>
		<p class="text-xs text-muted-foreground">{m.common_size()}: {bytes.format(metadata.size, { unitSeparator: ' ' })}</p>
	</div>
{/if}
