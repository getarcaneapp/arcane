<script lang="ts">
	import type { Snippet } from 'svelte';

	import FormInput from '#lib/components/form/form-input.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { WorkspaceFileEntry } from '#lib/types/workspace.js';
	import type { WorkspaceDisplayEntry } from '#lib/utils/workspace-files.js';

	import type { GitOpsSyncFormState } from './gitops-sync-form.svelte';
	import GitOpsSyncScheduleFields from './gitops-sync-schedule-fields.svelte';

	type Props = {
		form: GitOpsSyncFormState;
		isEditMode: boolean;
		selectedProjectId: string;
		backupProjectOptions: { value: string; label: string }[];
		backupNamePlaceholder: string;
		suggestedBackupDirectory: string;
		workspaceFiles: WorkspaceFileEntry[];
		loadingWorkspace: boolean;
		isNodeChecked: (node: WorkspaceDisplayEntry) => boolean;
		isNodeLocked: (node: WorkspaceDisplayEntry) => boolean;
		onToggleNode: (node: WorkspaceDisplayEntry, checked: boolean) => void;
		isEnvFile: (name: string) => boolean;
		hasSelectedEnvFile: boolean;
		backupPaths: string[];
		showBackupSummary: boolean;
		backupSummaryDestination: string;
		source: Snippet<[boolean]>;
	};

	let {
		form,
		isEditMode,
		selectedProjectId,
		backupProjectOptions,
		backupNamePlaceholder,
		suggestedBackupDirectory,
		workspaceFiles,
		loadingWorkspace,
		isNodeChecked,
		isNodeLocked,
		onToggleNode,
		isEnvFile,
		hasSelectedEnvFile,
		backupPaths,
		showBackupSummary,
		backupSummaryDestination,
		source
	}: Props = $props();

	const inputs = $derived(form.inputs);
</script>

{#snippet envFileBadge(entry: WorkspaceDisplayEntry)}
	{#if !entry.isDirectory && isEnvFile(entry.name)}
		<span class="shrink-0 rounded bg-warning/10 px-1.5 py-0.5 text-3xs text-warning">
			{m.environment_file()}
		</span>
	{/if}
{/snippet}

<div class="grid gap-3 sm:grid-cols-2">
	<SelectWithLabel
		id="backupProject"
		label={m.project()}
		value={selectedProjectId}
		options={backupProjectOptions}
		disabled={isEditMode}
		error={inputs.projectId.error}
		onValueChange={(value) => {
			if (!value) return;
			if (value !== selectedProjectId) inputs.includeEnvFile.value = false;
			inputs.projectId.value = value;
		}}
	/>

	<FormInput label={m.git_sync_name()} type="text" placeholder={backupNamePlaceholder} bind:input={inputs.name} />
</div>

{@render source(false)}

<div class="space-y-2">
	<Label for="backupDirectory">{m.destination_folder()}</Label>
	{#if isEditMode}
		<Input id="backupDirectory" type="text" value={inputs.backupDirectory.value} readonly />
		<p class="text-xs text-muted-foreground">{m.destination_folder_locked()}</p>
	{:else}
		<Input
			id="backupDirectory"
			type="text"
			placeholder={suggestedBackupDirectory}
			bind:value={inputs.backupDirectory.value}
			aria-invalid={inputs.backupDirectory.error ? 'true' : undefined}
		/>
		<p class="text-xs text-muted-foreground">{m.destination_folder_description()}</p>
	{/if}
	{#if inputs.backupDirectory.error}
		<p class="text-xs font-medium text-destructive">{inputs.backupDirectory.error}</p>
	{/if}
</div>

<div class="space-y-2">
	<div class="flex h-56 flex-col overflow-hidden rounded-md border border-border/50">
		{#if !selectedProjectId}
			<p class="px-3 py-2 text-xs text-muted-foreground">{m.project_required()}</p>
		{:else if loadingWorkspace}
			<div class="flex items-center gap-2 px-3 py-2">
				<Spinner class="size-4" />
				<span class="text-sm text-muted-foreground">{m.common_loading()}</span>
			</div>
		{:else}
			<WorkspaceFileTreePanel
				title={m.included_files()}
				entries={workspaceFiles}
				selectable
				isChecked={isNodeChecked}
				isSelectionLocked={isNodeLocked}
				onCheckedChange={onToggleNode}
				rowBadge={envFileBadge}
				emptyMessage={m.no_project_files()}
			/>
		{/if}
	</div>
	<p class="text-xs text-muted-foreground">{m.included_files_description()}</p>
	{#if hasSelectedEnvFile}
		<p class="text-xs text-warning">{m.environment_file_warning()}</p>
	{/if}
</div>

<div class="divide-y divide-border/50 border-y border-border/50">
	<GitOpsSyncScheduleFields
		{form}
		switchId="backupAutoSyncSwitch"
		intervalId="backupSyncInterval"
		label={m.automatic_backup()}
		description={m.automatic_backup_description()}
	/>

	<div class="flex items-center justify-between gap-4 py-3">
		<div class="min-w-0">
			<Label for="backupOnSaveSwitch" class="mb-0">{m.back_up_on_save()}</Label>
			<p class="text-xs text-muted-foreground">{m.back_up_on_save_description()}</p>
		</div>
		<Switch id="backupOnSaveSwitch" bind:checked={inputs.backupOnSave.value} disabled={!inputs.autoSync.value} />
	</div>

	<div class="py-3">
		<div class="flex items-center justify-between gap-4">
			<div class="min-w-0">
				<Label for="includeEnvFileSwitch" class="mb-0">{m.include_environment_file()}</Label>
				<p class="text-xs text-muted-foreground">{m.include_environment_file_description()}</p>
			</div>
			<Switch id="includeEnvFileSwitch" bind:checked={inputs.includeEnvFile.value} />
		</div>
		{#if inputs.includeEnvFile.value}
			<p class="mt-2 text-xs text-warning">{m.environment_file_warning()}</p>
		{/if}
	</div>
</div>

{#if showBackupSummary}
	<div class="space-y-1 text-xs text-muted-foreground">
		<p>
			{m.commits_files_to({ count: backupPaths.length })}
			<span class="font-mono break-all text-foreground">{backupSummaryDestination}</span>
		</p>
		<p class="font-mono break-all">{backupPaths.join(', ')}</p>
	</div>
{/if}
