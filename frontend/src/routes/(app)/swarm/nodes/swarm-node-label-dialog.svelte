<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '$lib/utils/api';

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
			await onAdd(key.trim(), value.trim());
			handleCancel();
		} catch (err) {
			toast.error(m.common_update_failed({ resource: m.common_labels() }) + ': ' + extractApiErrorMessage(err));
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

<ResponsiveDialog.Root
	bind:open
	title={m.swarm_service_form_add_label()}
	description={m.common_labels_description({ resource: m.swarm_node() })}
>
	<form onsubmit={handleSubmit} class="space-y-4 px-6 py-4">
		<div class="space-y-2">
			<Label for="label-key" class={isReservedPrefix ? 'text-red-500' : ''}>Key</Label>
			<Input
				id="label-key"
				bind:value={key}
				placeholder={m.swarm_service_form_key_placeholder()}
				required
				class={isReservedPrefix ? 'border-red-500 focus-visible:ring-red-500' : ''}
			/>
			{#if isReservedPrefix}
				<p class="text-[11px] font-medium text-red-500">
					Prefixes 'engine.labels' and 'com.docker.swarm' are reserved for system use.
				</p>
			{/if}
		</div>
		<div class="space-y-2">
			<Label for="label-value">Value</Label>
			<Input id="label-value" bind:value placeholder={m.swarm_service_form_value_placeholder()} />
		</div>
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
