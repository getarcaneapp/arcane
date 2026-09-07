<script lang="ts">
	import * as m from '#lib/paraglide/messages.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import type { BackupRun } from '#lib/types/backup.js';
	import { backupDestinationLabel, backupDestinationName } from '#lib/utils/backups.js';

	let { item }: { item: Pick<BackupRun, 'destination' | 's3DestinationId' | 's3DestinationName' | 'remoteAvailable'> } = $props();
</script>

<div class="flex items-center gap-2">
	<Badge variant={item.destination === 'local' ? 'gray' : 'blue'}>{backupDestinationLabel(item.destination)}</Badge>
	{#if item.destination !== 'local' && backupDestinationName(item)}
		<span class="max-w-48 truncate text-xs text-muted-foreground">{backupDestinationName(item)}</span>
	{/if}
	{#if item.remoteAvailable === false}
		<span class="text-xs text-destructive">{m.backups_remote_unavailable()}</span>
	{/if}
</div>
