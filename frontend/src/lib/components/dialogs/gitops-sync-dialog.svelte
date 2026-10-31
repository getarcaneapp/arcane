<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import GitopsDialogFooter from '#lib/components/dialogs/gitops-dialog-footer.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import type { GitOpsSync, GitOpsSyncCreate, GitOpsSyncMode, GitOpsSyncUpdate } from '#lib/types/automation.js';

	import GitOpsSyncForm from './gitops-sync-form.svelte';

	type GitOpsSyncDialogProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		environmentId: string;
		targetType?: string;
		mode?: GitOpsSyncMode;
		projectId?: string;
		onSubmit: (detail: { sync: GitOpsSyncCreate | GitOpsSyncUpdate; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		syncToEdit,
		environmentId = $bindable(),
		targetType,
		mode,
		projectId,
		onSubmit,
		isLoading
	}: GitOpsSyncDialogProps = $props();

	const isEditMode = $derived(!!syncToEdit);
	let selectedMode = $state<GitOpsSyncMode>(untrack(() => syncToEdit?.mode ?? mode ?? 'deploy'));
	const isBackupMode = $derived(selectedMode === 'backup');

	const settingsQuery = createQuery(() => {
		const requestEnvironmentId = environmentId;
		return {
			queryKey: queryKeys.settings.byEnvironment(requestEnvironmentId),
			queryFn: () => settingsService.getReadableSettings(requestEnvironmentId),
			enabled: open
		};
	});
</script>

<ResponsiveDialog
	bind:open
	title={isEditMode ? m.git_sync_edit_title() : m.git_sync_add_title()}
	description={isEditMode ? m.common_edit_description() : m.git_sync_add_description()}
	contentClass="sm:max-w-3xl"
>
	{#snippet children()}
		{#if settingsQuery.data}
			{#key environmentId}
				<GitOpsSyncForm
					{open}
					{syncToEdit}
					bind:environmentId
					bind:mode={selectedMode}
					{targetType}
					{projectId}
					settings={settingsQuery.data.values}
					{onSubmit}
				/>
			{/key}
		{:else}
			<div class="flex items-center justify-center py-8">
				<Spinner class="size-6" />
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<GitopsDialogFooter cancelLabel={m.common_cancel()} {isLoading} onCancel={() => (open = false)}>
			{#snippet primary()}
				<Button type="submit" form="sync-form" class="flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{#if isEditMode && isBackupMode}
						{m.common_save()}
					{:else if isEditMode}
						{m.common_save_changes()}
					{:else if isBackupMode}
						{m.save_and_back_up()}
					{:else}
						{m.common_add_button({ resource: m.resource_sync_cap() })}
					{/if}
				</Button>
			{/snippet}
		</GitopsDialogFooter>
	{/snippet}
</ResponsiveDialog>
