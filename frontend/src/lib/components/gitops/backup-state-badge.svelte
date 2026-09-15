<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import type { BadgeVariant } from '#lib/components/ui/badge/badge.svelte';
	import type { GitOpsBackupState, GitOpsSync } from '#lib/types/automation.js';
	import { m } from '#lib/paraglide/messages.js';

	let { sync }: { sync: GitOpsSync } = $props();

	const state = $derived<GitOpsBackupState>(sync.backupState ?? 'never');

	const label = $derived.by(() => {
		switch (state) {
			case 'pending':
				return m.pending_changes();
			case 'backing_up':
				return m.backing_up();
			case 'backed_up':
				return m.backed_up();
			case 'paused':
				return m.paused();
			case 'failed':
				return m.common_failed();
			case 'needs_attention':
				return m.needs_attention();
			default:
				return m.not_backed_up_yet();
		}
	});

	const variant = $derived.by<BadgeVariant>(() => {
		switch (state) {
			case 'pending':
				return 'amber';
			case 'backing_up':
				return 'blue';
			case 'backed_up':
				return 'green';
			case 'failed':
				return 'red';
			case 'needs_attention':
				return 'orange';
			default:
				return 'gray';
		}
	});
</script>

<Badge {variant} size="sm">{label}</Badge>
