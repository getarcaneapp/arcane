<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { m } from '#lib/paraglide/messages.js';

	import type { BuildFormInputs, BuildProviderOption } from './build-form.types';

	let {
		inputs = $bindable(),
		provider,
		providerOptions,
		environmentId,
		isBuilding = false,
		onBuild
	}: {
		inputs: BuildFormInputs;
		environmentId: string;
		provider: 'local' | 'depot';
		providerOptions: BuildProviderOption[];
		isBuilding?: boolean;
		onBuild: () => void;
	} = $props();
</script>

<div class="flex flex-wrap items-center justify-end gap-2">
	{#if providerOptions.length > 1}
		<SelectWithLabel
			id="build-provider"
			label={m.build_provider()}
			hideLabel
			compact
			options={providerOptions}
			bind:value={() => provider, (value) => (inputs.provider.value = value === 'depot' ? 'depot' : 'local')}
		/>
	{/if}

	<IfPermitted perm="images:build" envId={environmentId}>
		<ArcaneButton action="build" type="button" onclick={onBuild} loading={isBuilding} disabled={isBuilding} />
	</IfPermitted>
</div>
