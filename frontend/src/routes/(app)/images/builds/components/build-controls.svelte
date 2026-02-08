<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { BuildFormInputsStore, BuildProviderOption } from './build-form.types';

	let {
		inputs,
		providerOptions,
		selectedProviderLabel,
		isBuilding = false,
		onBuild
	}: {
		inputs: BuildFormInputsStore;
		providerOptions: BuildProviderOption[];
		selectedProviderLabel: string;
		isBuilding?: boolean;
		onBuild?: () => void;
	} = $props();
</script>

<div class="flex flex-wrap items-center justify-end gap-3">
	<Select.Root type="single" bind:value={$inputs.provider.value}>
		<Select.Trigger size="sm" class="w-[160px]">
			<span class="truncate">{selectedProviderLabel}</span>
		</Select.Trigger>
		<Select.Content>
			{#each providerOptions as option (option.value)}
				<Select.Item value={option.value}>
					<div class="flex flex-col items-start gap-0.5">
						<span class="font-medium">{option.label}</span>
						{#if option.description}
							<span class="text-muted-foreground text-xs">{option.description}</span>
						{/if}
					</div>
				</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>

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
</div>
