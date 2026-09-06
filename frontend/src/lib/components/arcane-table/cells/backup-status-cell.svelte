<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import type { BackupStatus } from '#lib/types/backup.js';
	import { backupStatusLabel, backupStatusVariant } from '#lib/utils/backups.js';

	let { status }: { status: BackupStatus } = $props();
</script>

<Badge variant={backupStatusVariant(status)}>{backupStatusLabel(status)}</Badge>
