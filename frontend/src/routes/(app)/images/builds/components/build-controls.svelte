<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
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
		onBuild?: () => void;
	} = $props();
</script>

<div class="flex flex-wrap items-center justify-end gap-3">
	<SelectWithLabel
		id="build-provider"
		label={m.build_provider()}
		hideLabel
		triggerSize="sm"
		compact
		options={providerOptions}
		bind:value={
			() => provider,
			(value) => {
				if (value === 'depot') inputs.provider.value = 'depot';
				else inputs.provider.value = 'local';
			}
		}
	/>

	<div class="hidden h-6 w-px bg-border lg:block"></div>

	<IfPermitted perm="images:push" envId={environmentId}>
		<div class="flex items-center gap-2">
			<Switch id="build-push" checked={inputs.push.value} onCheckedChange={(v) => (inputs.push.value = v === true)} />
			<Label for="build-push">{m.push()}</Label>
		</div>
	</IfPermitted>

	<div class="flex items-center gap-2">
		<Switch
			id="build-load"
			checked={inputs.load.value}
			onCheckedChange={(v) => (inputs.load.value = v === true)}
			disabled={provider === 'depot'}
		/>
		<Label for="build-load">{m.load()}</Label>
	</div>

	<IfPermitted perm="images:build" envId={environmentId}>
		<ArcaneButton
			action="start_all"
			type="button"
			hoverEffect="lift"
			customLabel={m.build()}
			onclick={() => onBuild?.()}
			loading={isBuilding}
			disabled={isBuilding}
		/>
	</IfPermitted>
</div>
