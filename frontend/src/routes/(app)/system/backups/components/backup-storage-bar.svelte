<script lang="ts">
	import type { ClassValue } from 'svelte/elements';

	import { cn } from '#lib/utils.js';
	import { backupStorageSegments, type BackupStorageTotals } from '#lib/utils/backups.js';

	interface Props {
		stats: Pick<BackupStorageTotals, 'storedBytes' | 'rawBytes' | 'logicalBytes'>;
		size?: 'default' | 'slim';
		class?: ClassValue;
	}

	let { stats, size = 'default', class: className }: Props = $props();

	const segments = $derived.by(() => {
		const { totalBytes, storedBytes, compressionBytes, deduplicationBytes } = backupStorageSegments(stats);
		if (totalBytes <= 0) return [];
		return [
			{ key: 'stored', bytes: storedBytes, class: 'bg-primary' },
			{ key: 'compression', bytes: compressionBytes, class: 'bg-saved-hatch' },
			{ key: 'deduplication', bytes: deduplicationBytes, class: 'bg-deduplicated-hatch' }
		]
			.filter((segment) => segment.bytes > 0)
			.map((segment) => ({ ...segment, share: Math.max((segment.bytes / totalBytes) * 100, 2) }));
	});
</script>

<div
	class={cn('flex w-full overflow-hidden rounded-full bg-muted', size === 'slim' ? 'h-1.5 gap-px' : 'h-3 gap-0.5', className)}
	aria-hidden="true"
>
	{#each segments as segment (segment.key)}
		<span class={cn('h-full grow-(--share) basis-0', segment.class)} style:--share={segment.share}></span>
	{/each}
</div>
