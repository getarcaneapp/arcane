<script lang="ts">
	import { Badge } from '#lib/components/ui/badge';
	import type { BackupStatus } from '#lib/types/backup';
	import { backupStatusLabel, backupStatusVariant } from '#lib/utils/backups';

	let { status }: { status: BackupStatus } = $props();
</script>

<Badge variant={backupStatusVariant(status)}>{backupStatusLabel(status)}</Badge>
