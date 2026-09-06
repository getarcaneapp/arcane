<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import type { BackupManagementType } from '#lib/types/backup.js';
	import { backupManagementLabel } from '#lib/utils/backups.js';

	let { type }: { type?: BackupManagementType } = $props();
</script>

<Badge variant="purple">{backupManagementLabel(type)}</Badge>
