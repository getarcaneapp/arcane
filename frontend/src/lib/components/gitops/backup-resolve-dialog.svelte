<script lang="ts">
	import ResponsiveDialog from '#lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';
	import type { GitOpsBackupFileChange, GitOpsSync } from '#lib/types/automation.js';

	let {
		open = $bindable(false),
		environmentId,
		sync,
		onResolved
	}: {
		open?: boolean;
		environmentId: string;
		sync: GitOpsSync | null;
		onResolved?: () => void;
	} = $props();

	const queryClient = useQueryClient();

	let resolving = $state(false);

	const preview = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.backupPreview(environmentId, sync?.id ?? ''),
		queryFn: () => gitOpsSyncService.previewBackup(environmentId, sync!.id),
		enabled: open && !!sync
	}));

	const stateLabel = $derived.by(() => {
		switch (preview.data?.state) {
			case 'conflict':
				return m.backup_files_changed_in_repository();
			case 'destination_occupied':
				return m.backup_folder_occupied();
			case 'changes':
				return m.pending_changes();
			default:
				return m.backup_up_to_date();
		}
	});

	function changeLabel(change: GitOpsBackupFileChange['change']): string {
		if (change === 'added') return m.added();
		if (change === 'removed') return m.removed();
		return m.modified();
	}

	function changeVariant(change: GitOpsBackupFileChange['change']): 'green' | 'red' | 'amber' {
		if (change === 'added') return 'green';
		if (change === 'removed') return 'red';
		return 'amber';
	}

	async function useArcaneFiles() {
		if (!sync) return;
		resolving = true;
		const result = await tryCatch(gitOpsSyncService.resolveBackupConflict(environmentId, sync.id, { strategy: 'use_arcane' }));
		await handleApiResultWithCallbacks({
			result,
			message: m.backup_resolve_failed(),
			setLoadingState: (value) => (resolving = value),
			onSuccess: async () => {
				toast.success(m.backup_resolved());
				await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.all });
				open = false;
				onResolved?.();
			}
		});
		resolving = false;
	}
</script>

{#snippet fileList(title: string, items: GitOpsBackupFileChange[])}
	<div class="space-y-2">
		<h4 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{title}</h4>
		<ul class="divide-y divide-border/50 rounded-lg border border-border/50">
			{#each items as item (item.path)}
				<li class="flex items-center justify-between gap-3 px-3 py-2">
					<span class="font-mono text-xs break-all">{item.path}</span>
					<Badge variant={changeVariant(item.change)} size="sm">{changeLabel(item.change)}</Badge>
				</li>
			{/each}
		</ul>
	</div>
{/snippet}

<ResponsiveDialog bind:open title={m.resolve_backup()} description={sync?.name} contentClass="sm:max-w-xl">
	<div class="pb-6">
		{#if preview.isPending}
			<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				{m.common_loading()}
			</div>
		{:else if preview.isError}
			<p class="py-8 text-sm text-destructive">{m.backup_preview_load_failed()}</p>
		{:else}
			<div class="space-y-4">
				<p class="text-sm font-medium">{stateLabel}</p>
				<p class="text-sm text-muted-foreground">{m.use_arcane_files_description()}</p>

				{#if (preview.data?.conflicts ?? []).length > 0}
					{@render fileList(m.conflicts(), preview.data?.conflicts ?? [])}
				{/if}

				{#if (preview.data?.changes ?? []).length > 0}
					{@render fileList(m.changes(), preview.data?.changes ?? [])}
				{:else}
					<p class="text-sm text-muted-foreground">{m.no_pending_changes()}</p>
				{/if}
			</div>
		{/if}
	</div>
	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" customLabel={m.common_cancel()} icon={null} onclick={() => (open = false)} />
		<ArcaneButton
			action="base"
			tone="outline-primary"
			customLabel={m.use_arcane_files()}
			icon={null}
			loading={resolving}
			disabled={resolving || !sync}
			onclick={useArcaneFiles}
		/>
	{/snippet}
</ResponsiveDialog>
