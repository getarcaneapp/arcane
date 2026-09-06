<script lang="ts">
	import type { BackupTrigger } from '#lib/types/backup.js';
	import { backupTriggerLabel } from '#lib/utils/backups.js';

	let { trigger }: { trigger: BackupTrigger } = $props();
</script>

{backupTriggerLabel(trigger)}
