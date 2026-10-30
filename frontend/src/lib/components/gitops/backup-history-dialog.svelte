<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import type { CodeBlockLineVariant } from '#lib/components/ui/code-block/index.js';
	import ResponsiveDialog from '#lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import type { GitOpsBackupHistoryEntry, GitOpsSync } from '#lib/types/automation.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';

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

	function diffLines(patch: string): { code: string; variant: CodeBlockLineVariant }[] {
		return (patch.endsWith('\n') ? patch.slice(0, -1) : patch).split('\n').map((line) => {
			if (line.startsWith('+++') || line.startsWith('---')) {
				return { code: line, variant: 'default' };
			}
			if (line.startsWith('+')) {
				return { code: line.slice(1), variant: 'added' };
			}
			if (line.startsWith('-')) {
				return { code: line.slice(1), variant: 'removed' };
			}
			return { code: line.startsWith(' ') ? line.slice(1) : line, variant: 'default' };
		});
	}

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
>
	<div class="pb-6">
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
												<CodeBlock.Root size="sm">
													<CodeBlock.Content>
														{#each diffLines(diff.patch) as line, index (index)}
															<CodeBlock.Line code={line.code} variant={line.variant} />
														{/each}
													</CodeBlock.Content>
												</CodeBlock.Root>
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
	</div>
	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" customLabel={m.common_close()} icon={null} onclick={() => (open = false)} />
	{/snippet}
</ResponsiveDialog>
