<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	type SwarmNodeLabelDialogProps = {
		open: boolean;
		onAdd: (key: string, value: string) => void | Promise<void>;
	};

	let { open = $bindable(false), onAdd }: SwarmNodeLabelDialogProps = $props();

	let key = $state('');
	let value = $state('');
	let isSubmitting = $state(false);

	const isReservedPrefix = $derived(key.trim().startsWith('engine.labels') || key.trim().startsWith('com.docker.swarm'));

	async function handleSubmit(e?: Event) {
		e?.preventDefault();
		if (!key.trim() || isReservedPrefix) return;
		isSubmitting = true;
		try {
			const operationResult = await tryCatch(
				(async () => {
					await onAdd(key.trim(), value.trim());
					handleCancel();
				})()
			);
			if (operationResult.error !== null) {
				const err = operationResult.error;

				toast.error(m.common_update_failed({ resource: m.common_labels() }) + ': ' + extractApiErrorMessage(err));
			}
		} finally {
			isSubmitting = false;
		}
	}

	function handleCancel() {
		open = false;
		key = '';
		value = '';
	}
</script>

<ResponsiveDialog.Root bind:open title={m.add_label()} description={m.common_labels_description({ resource: m.swarm_node() })}>
	<form onsubmit={handleSubmit} class="space-y-4 px-6 py-4">
		<div class="space-y-2">
			<Label for="label-key" variant={isReservedPrefix ? 'destructive' : 'default'}>{m.swarm_node_label_key()}</Label>
			<Input
				id="label-key"
				bind:value={key}
				placeholder={m.swarm_service_form_key_placeholder()}
				required
				aria-invalid={!!isReservedPrefix}
			/>
			{#if isReservedPrefix}
				<p class="text-2xs font-medium text-destructive">
					{m.swarm_node_label_reserved_prefixes()}
				</p>
			{/if}
		</div>
		<div class="space-y-2">
			<Label for="label-value">{m.swarm_node_label_value()}</Label>
			<Input id="label-value" bind:value placeholder={m.value_placeholder()} />
		</div>
		<button type="submit" class="hidden" aria-label={m.add_label()}></button>
	</form>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 px-6 pb-6 sm:flex-row sm:justify-end">
			<ArcaneButton action="base" tone="outline" customLabel={m.common_cancel()} onclick={handleCancel} disabled={isSubmitting} />
			<ArcaneButton
				action="base"
				customLabel={m.common_add_button({ resource: m.common_labels() })}
				onclick={handleSubmit}
				loading={isSubmitting}
				disabled={!key.trim() || isReservedPrefix || isSubmitting}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
