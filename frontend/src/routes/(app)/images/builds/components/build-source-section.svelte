<script lang="ts">
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { BuildWorkspaceDraft } from '../build-workspace-draft.svelte';
	import BuildFolderPicker from './build-folder-picker.svelte';
	import BuildRemoteContext from './build-remote-context.svelte';

	let {
		workspace,
		contextDir,
		buildsRoot,
		loading,
		contextMode = $bindable(),
		remoteContext = $bindable(),
		onEditFiles
	}: {
		workspace: BuildWorkspaceDraft;
		contextDir: string;
		buildsRoot: string;
		loading: boolean;
		contextMode: 'workspace' | 'remote';
		remoteContext: string;
		onEditFiles: () => void;
	} = $props();

	const modeOptions = $derived([
		{ value: 'workspace', label: m.workspace_folder() },
		{ value: 'remote', label: m.git_repository() }
	]);
</script>

<section class="space-y-3">
	<h2 class="text-sm font-semibold">{m.source()}</h2>
	<Tabs.Root value={contextMode} onValueChange={(value) => (contextMode = value === 'remote' ? 'remote' : 'workspace')}>
		<Tabs.List class="min-h-8 w-full sm:w-fit" aria-label={m.build_context()}>
			{#each modeOptions as option (option.value)}
				<Tabs.Trigger value={option.value} size="sm" class="h-7 flex-1 sm:flex-none">{option.label}</Tabs.Trigger>
			{/each}
		</Tabs.List>
	</Tabs.Root>

	{#if contextMode === 'remote'}
		<BuildRemoteContext bind:value={remoteContext} />
	{:else}
		<div class="space-y-2">
			<Label for="build-context-dir">{m.build_context()}</Label>
			<div class="flex items-center gap-2">
				<Input id="build-context-dir" value={contextDir} readonly mono size="sm" class="min-w-0 flex-1" />
				<BuildFolderPicker {workspace} {buildsRoot} disabled={loading} />
			</div>
			<button
				type="button"
				class="text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
				onclick={onEditFiles}
			>
				{m.build_edit_files_hint()}
			</button>
		</div>
	{/if}
</section>
