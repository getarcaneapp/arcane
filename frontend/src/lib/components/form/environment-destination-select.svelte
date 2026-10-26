<script lang="ts">
	import { Select as SelectPrimitive } from 'bits-ui';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { ArrowDownIcon, AgentConnectionIcon, EnvironmentsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentColor } from '#lib/stores/environment-colors.store.svelte.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';

	let {
		value = $bindable(),
		permission,
		environments,
		variant = 'toolbar',
		size = 'default'
	}: {
		value?: string;
		permission?: string;
		environments?: EnvironmentRef[];
		variant?: 'toolbar' | 'field';
		size?: 'sm' | 'default';
	} = $props();

	const permitted = $derived(
		(environments ?? environmentStore.shown).filter((env) => !permission || hasPermission(permission, env.id))
	);
	const options = $derived(permitted.map((env) => ({ value: env.id, label: env.name })));
	const currentId = $derived(value ?? options[0]?.value ?? '');
	const current = $derived(permitted.find((env) => env.id === currentId));

	function pick(environmentId: string) {
		value = environmentId;
	}

	function environmentIcon(environmentId: string) {
		if (environmentId === LOCAL_DOCKER_ENVIRONMENT_ID) return EnvironmentsIcon;
		return AgentConnectionIcon;
	}
</script>

{#snippet environmentGlyph(environmentId: string)}
	{@const Icon = environmentIcon(environmentId)}
	<span
		class="environment-glyph inline-flex shrink-0"
		style:--environment-color={environmentColor(environmentId) ?? 'var(--muted-foreground)'}
	>
		<Icon class="size-4" />
	</span>
{/snippet}

{#if variant === 'toolbar'}
	<Label for="page-environment" class="sr-only">{m.resource_environment_cap()}</Label>
	<Select.Root type="single" bind:value={() => currentId, pick}>
		<SelectPrimitive.Trigger id="page-environment">
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" {size} title={current?.name} class="shrink-0">
					{#if current}
						{@render environmentGlyph(current.id)}
						<span class="hidden max-w-48 truncate sm:inline">{current.name}</span>
					{/if}
					<ArrowDownIcon class="size-3.5 opacity-60" />
				</ArcaneButton>
			{/snippet}
		</SelectPrimitive.Trigger>
		<Select.Content class="w-auto max-w-72 min-w-48">
			{#each permitted as env (env.id)}
				<Select.Item value={env.id} label={env.name}>
					{@render environmentGlyph(env.id)}
					<span class="truncate">{env.name}</span>
				</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>
{:else}
	<SelectWithLabel
		id="destination-environment"
		label={m.resource_environment_cap()}
		bind:value={() => currentId, pick}
		{options}
	/>
{/if}

<style>
	.environment-glyph {
		color: color-mix(in srgb, var(--environment-color) 75%, var(--foreground));
	}
</style>
