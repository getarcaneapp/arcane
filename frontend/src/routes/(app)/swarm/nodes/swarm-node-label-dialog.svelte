<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '$lib/utils/api.util';

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
			open = false;
			key = '';
			value = '';
		} catch (err) {
			toast.error('Failed to add label: ' + extractApiErrorMessage(err));
		} finally {
			isSubmitting = false;
		}
	}
</script>

<ResponsiveDialog.Root bind:open title="Add Node Label" description="Add a new custom label to the swarm node.">
	<form onsubmit={handleSubmit} class="space-y-4 px-6 py-4">
		<div class="space-y-2">
			<Label for="label-key" class={isReservedPrefix ? 'text-red-500' : ''}>Key</Label>
			<Input
				id="label-key"
				bind:value={key}
				placeholder="e.g. storage"
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
			<Input id="label-value" bind:value placeholder="e.g. ssd" />
		</div>
		<button type="submit" class="hidden"></button>
	</form>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 px-6 pb-6 sm:flex-row sm:justify-end">
			<ArcaneButton
				action="base"
				tone="outline"
				customLabel={m.common_cancel()}
				onclick={() => (open = false)}
				disabled={isSubmitting}
			/>
			<ArcaneButton
				action="base"
				customLabel={m.common_add_button({ resource: 'Label' })}
				onclick={handleSubmit}
				loading={isSubmitting}
				disabled={!key.trim() || isReservedPrefix || isSubmitting}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
