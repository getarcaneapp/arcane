<script lang="ts">
	import ResponsiveDialog from '#lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { GitOpsBackupHistoryEntry, GitOpsSync } from '#lib/types/automation.js';

	let {
		open = $bindable(false),
		environmentId,
		sync
	}: {
		open?: boolean;
		environmentId: string;
		sync: GitOpsSync | null;
	} = $props();

	let selectedCommit = $state<string | null>(null);

	const history = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.backupHistory(environmentId, sync?.id ?? ''),
		queryFn: () => gitOpsSyncService.getBackupHistory(environmentId, sync!.id),
		enabled: open && !!sync
	}));

	const revision = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.backupRevision(environmentId, sync?.id ?? '', selectedCommit ?? ''),
		queryFn: () => gitOpsSyncService.getBackupRevision(environmentId, sync!.id, selectedCommit!),
		enabled: open && !!sync && !!selectedCommit
	}));

	const entries = $derived<GitOpsBackupHistoryEntry[]>(history.data?.entries ?? []);

	function firstLine(message: string): string {
		return message.split('\n')[0] ?? message;
	}

	function handleOpenChange(next: boolean) {
		if (!next) selectedCommit = null;
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={handleOpenChange}
	title={m.backup_history()}
	description={sync?.name}
	contentClass="sm:max-w-3xl"
	class="pb-6"
>
	{#if history.isPending}
		<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
			<Spinner class="size-4" />
			{m.common_loading()}
		</div>
	{:else if history.isError}
		<p class="py-8 text-sm text-destructive">{m.backup_history_load_failed()}</p>
	{:else if entries.length === 0}
		<p class="py-8 text-sm text-muted-foreground">{m.backup_history_empty()}</p>
	{:else}
		<ul class="divide-y divide-border/50 rounded-lg border border-border/50">
			{#each entries as entry (entry.commit)}
				{@const isSelected = selectedCommit === entry.commit}
				<li>
					<button
						type="button"
						class="flex w-full flex-col gap-1 px-3 py-2.5 text-left hover:bg-muted/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
						aria-expanded={isSelected}
						onclick={() => (selectedCommit = isSelected ? null : entry.commit)}
					>
						<span class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
							<code class="rounded bg-muted px-1.5 py-0.5 font-mono">{entry.commit.slice(0, 7)}</code>
							<span>{formatDateTimeShort(entry.date)}</span>
							<span>{entry.author}</span>
							<span>{m.changed_files_count({ count: entry.files.length })}</span>
						</span>
						<span class="text-sm font-medium break-words">{firstLine(entry.message)}</span>
					</button>

					{#if isSelected}
						<div class="border-t border-border/50 bg-muted/20 px-3 py-3">
							{#if revision.isPending}
								<div class="flex items-center gap-2 text-sm text-muted-foreground">
									<Spinner class="size-4" />
									{m.common_loading()}
								</div>
							{:else if revision.isError}
								<p class="text-sm text-destructive">{m.backup_revision_load_failed()}</p>
							{:else if (revision.data?.diffs ?? []).length === 0}
								<p class="text-sm text-muted-foreground">{m.no_file_changes()}</p>
							{:else}
								<div class="space-y-3">
									{#each revision.data?.diffs ?? [] as diff (diff.path)}
										<div class="space-y-1">
											<p class="font-mono text-xs font-medium break-all">{diff.path}</p>
											<pre
												class="overflow-x-auto rounded-md border border-border/50 bg-background p-3 font-mono text-xs leading-relaxed whitespace-pre">{diff.patch}</pre>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" customLabel={m.common_close()} icon={null} onclick={() => (open = false)} />
	{/snippet}
</ResponsiveDialog>
