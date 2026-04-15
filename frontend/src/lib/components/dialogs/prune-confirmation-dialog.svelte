<script lang="ts">
	import PruneConfirmationDialogContent from '$lib/components/dialogs/prune-confirmation-dialog-content.svelte';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { SystemPruneRequest } from '$lib/types/prune.type';
	import type { Settings } from '$lib/types/settings.type';

	interface Props {
		open: boolean;
		isPruning?: boolean;
		defaults?: Settings | null;
		onConfirm?: (request: SystemPruneRequest) => void;
		onCancel?: () => void;
	}

	let { open = $bindable(), isPruning = false, defaults = null, onConfirm = () => {}, onCancel = () => {} }: Props = $props();

	function handleCancelInternal() {
		if (!isPruning) {
			onCancel();
		}
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={(isOpen) => !isOpen && handleCancelInternal()}
	title={m.prune_confirm_system_title()}
	description={m.prune_confirm_description()}
	contentClass="sm:max-w-[860px]"
>
	{#if open}
		<PruneConfirmationDialogContent {defaults} {isPruning} {onConfirm} onCancel={handleCancelInternal} />
	{/if}
</ResponsiveDialog>
