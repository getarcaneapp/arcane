<script lang="ts">
	import PruneConfirmationDialogContent from '#lib/components/dialogs/prune-confirmation-dialog-content.svelte';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SystemPruneRequest } from '#lib/types/automation.js';
	import type { Settings } from '#lib/types/settings.js';

	interface Props {
		open: boolean;
		isPruning?: boolean;
		defaults?: Settings | null;
		onConfirm?: (request: SystemPruneRequest) => void;
		onCancel?: () => void;
		environments?: { id: string; name: string }[];
		environmentId?: string | null;
		onEnvironmentChange?: (environmentId: string) => void;
	}

	let {
		open = $bindable(),
		isPruning = false,
		defaults = null,
		onConfirm = () => {},
		onCancel = () => {},
		environments = [],
		environmentId = null,
		onEnvironmentChange = () => {}
	}: Props = $props();

	const selectedEnvironmentName = $derived(
		environments.find((environment) => environment.id === environmentId)?.name ?? m.all_environments()
	);

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
	contentClass="sm:max-w-215"
>
	{#if open}
		{#if environments.length > 1}
			<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
				<span class="text-sm font-medium">{m.resource_environment_cap()}</span>
				<Select.Root type="single" value={environmentId ?? 'all'} disabled={isPruning} onValueChange={onEnvironmentChange}>
					<Select.Trigger class="h-8 min-w-48">
						<span class="truncate">{selectedEnvironmentName}</span>
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="all">{m.all_environments()}</Select.Item>
						{#each environments as environment (environment.id)}
							<Select.Item value={environment.id}>{environment.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		{/if}
		{#key defaults}
			<PruneConfirmationDialogContent {defaults} {isPruning} {onConfirm} onCancel={handleCancelInternal} />
		{/key}
	{/if}
</ResponsiveDialog>
