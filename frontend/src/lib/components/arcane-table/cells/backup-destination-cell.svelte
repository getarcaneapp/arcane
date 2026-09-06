<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import type { BackupRun } from '#lib/types/backup.js';
	import { backupDestinationLabel, backupDestinationName } from '#lib/utils/backups.js';

	let { item }: { item: Pick<BackupRun, 'destination' | 's3DestinationId' | 's3DestinationName'> } = $props();
</script>

<div class="flex items-center gap-2">
	<Badge variant={item.destination === 'local' ? 'gray' : 'blue'}>{backupDestinationLabel(item.destination)}</Badge>
	{#if item.destination !== 'local' && backupDestinationName(item)}
		<span class="max-w-48 truncate text-xs text-muted-foreground">{backupDestinationName(item)}</span>
	{/if}
</div>
