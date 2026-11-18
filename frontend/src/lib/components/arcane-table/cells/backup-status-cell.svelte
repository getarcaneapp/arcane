<script lang="ts">
	import { Badge } from '@arcane/ui/badge';

	import type { BackupStatus } from '#lib/types/backup.js';
	import { backupStatusLabel, backupStatusVariant } from '#lib/utils/backups.js';

	let { status }: { status: BackupStatus } = $props();
</script>

<Badge variant={backupStatusVariant(status)}>{backupStatusLabel(status)}</Badge>
