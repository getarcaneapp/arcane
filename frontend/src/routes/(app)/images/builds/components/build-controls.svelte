<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Label } from '$lib/components/ui/label';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { m } from '$lib/paraglide/messages';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import type { BuildFormInputsStore, BuildProviderOption } from './build-form.types';

	let {
		inputs,
		providerOptions,
		isBuilding = false,
		onBuild
	}: {
		inputs: BuildFormInputsStore;
		providerOptions: BuildProviderOption[];
		isBuilding?: boolean;
		onBuild?: () => void;
	} = $props();
</script>

<div class="flex flex-wrap items-center justify-end gap-3">
	<SelectWithLabel
		id="build-provider"
		label={m.build_provider()}
		hideLabel
		triggerSize="sm"
		triggerClass="w-[160px]"
		options={providerOptions}
		bind:value={$inputs.provider.value}
	/>

	<div class="bg-border hidden h-6 w-px lg:block"></div>

	<div class="flex items-center gap-2">
		<Switch id="build-push" checked={$inputs.push.value} onCheckedChange={(v) => ($inputs.push.value = v === true)} />
		<Label for="build-push" class="text-sm">{m.push()}</Label>
	</div>

	<div class="flex items-center gap-2">
		<Switch
			id="build-load"
			checked={$inputs.load.value}
			onCheckedChange={(v) => ($inputs.load.value = v === true)}
			disabled={$inputs.provider.value === 'depot'}
		/>
		<Label for="build-load" class="text-sm">{m.load()}</Label>
	</div>

	<IfPermitted perm="images:build">
		<ArcaneButton
			action="start_all"
			type="button"
			size="sm"
			hoverEffect="lift"
			customLabel={m.build()}
			onclick={() => onBuild?.()}
			loading={isBuilding}
			disabled={isBuilding}
		/>
	</IfPermitted>
</div>
