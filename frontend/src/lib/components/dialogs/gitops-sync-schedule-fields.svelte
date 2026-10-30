<script lang="ts">
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { GitOpsSyncFormState } from './gitops-sync-form.svelte';

	type Props = {
		form: GitOpsSyncFormState;
		switchId: string;
		intervalId: string;
		label: string;
		description: string;
		showAutoSyncError?: boolean;
	};

	let { form, switchId, intervalId, label, description, showAutoSyncError = false }: Props = $props();

	const inputs = $derived(form.inputs);
</script>

<div class="flex items-center justify-between gap-4 py-3">
	<div class="min-w-0">
		<Label for={switchId} class="mb-0">{label}</Label>
		<p class="text-xs text-muted-foreground">{description}</p>
		{#if showAutoSyncError && inputs.autoSync.error}
			<p class="text-xs font-medium text-destructive">{inputs.autoSync.error}</p>
		{/if}
	</div>
	<div class="flex shrink-0 items-center gap-3">
		{#if inputs.autoSync.value}
			<label class="flex items-center gap-1.5 text-xs text-muted-foreground" for={intervalId}>
				{m.every()}
				<Input
					id={intervalId}
					type="number"
					min="1"
					class="h-8 w-16 text-center"
					bind:value={inputs.syncInterval.value}
					aria-invalid={inputs.syncInterval.error ? 'true' : undefined}
				/>
				{m.minutes()}
			</label>
		{/if}
		<Switch id={switchId} bind:checked={inputs.autoSync.value} />
	</div>
</div>
{#if inputs.syncInterval.error}
	<p class="pb-2 text-xs font-medium text-destructive">{inputs.syncInterval.error}</p>
{/if}
