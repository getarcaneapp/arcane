<script lang="ts">
	import type { BackupTrigger } from '#lib/types/backup';
	import { backupTriggerLabel } from '#lib/utils/backups';

	let { trigger }: { trigger: BackupTrigger } = $props();
</script>

{backupTriggerLabel(trigger)}
