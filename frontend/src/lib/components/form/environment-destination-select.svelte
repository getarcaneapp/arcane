<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { Select as SelectPrimitive } from 'bits-ui';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { ArrowDownIcon, EdgeConnectionIcon, EnvironmentsIcon, RemoteEnvironmentIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentColor } from '#lib/stores/environment-colors.store.svelte.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';

	// Picks one environment while several are shown. A bound `value` is a create's destination and never changes
	// the selected environment; callers derive it with destinationEnvironmentId, so it is always a permitted option.
	// Without `value`, pages that show one environment at a time pick the route's or selected one, or the first option.
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

	const selectsEnvironment = $derived(value === undefined);
	const permitted = $derived(
		(environments ?? environmentStore.shown).filter((env) => !permission || hasPermission(permission, env.id))
	);
	const options = $derived(permitted.map((env) => ({ value: env.id, label: env.name })));
	// Routes under /environments/[id] show that environment, which can differ from the selected one.
	const routeEnvironmentId = $derived(page.route.id?.includes('/environments/[id]') ? page.params.id : undefined);
	const pageEnvironmentId = $derived(
		options.find((option) => option.value === (routeEnvironmentId ?? environmentStore.selected?.id))?.value ??
			options[0]?.value ??
			''
	);
	const currentId = $derived(selectsEnvironment ? pageEnvironmentId : (value ?? ''));
	const current = $derived(permitted.find((env) => env.id === currentId));

	function pick(environmentId: string) {
		if (selectsEnvironment) {
			selectEnvironment(environmentId);
			return;
		}
		value = environmentId;
	}

	function selectEnvironment(environmentId: string) {
		const environment = environmentStore.available.find((env) => env.id === environmentId);
		if (!environment) return;
		// Picking the selected environment changes nothing in the store, so move the route to it directly.
		if (routeEnvironmentId && environment.id === environmentStore.selected?.id) {
			const pathname = page.url.pathname.replace(
				`/environments/${encodeURIComponent(routeEnvironmentId)}`,
				`/environments/${encodeURIComponent(environment.id)}`
			);
			void goto(`${pathname}${page.url.search}${page.url.hash}`);
			return;
		}
		void environmentStore.setEnvironment(environment);
	}

	function environmentIcon(environmentId: string) {
		if (environmentId === LOCAL_DOCKER_ENVIRONMENT_ID) return EnvironmentsIcon;
		if (environmentStore.available.find((env) => env.id === environmentId)?.isEdge) return EdgeConnectionIcon;
		return RemoteEnvironmentIcon;
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

{#if environmentStore.shown.length > 1}
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
{/if}

<style>
	.environment-glyph {
		color: color-mix(in srgb, var(--environment-color) 75%, var(--foreground));
	}
</style>
