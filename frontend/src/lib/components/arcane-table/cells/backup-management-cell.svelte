<script lang="ts">
	import { Badge } from '#lib/components/ui/badge';
	import type { BackupManagementType } from '#lib/types/backup';
	import { backupManagementLabel } from '#lib/utils/backups';

	let { type }: { type?: BackupManagementType } = $props();
</script>

<Badge variant="purple">{backupManagementLabel(type)}</Badge>
