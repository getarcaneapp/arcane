<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';

	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';

	// Picks one environment while several are shown. A bound `value` is a create's destination and never changes
	// the selected environment; callers derive it with destinationEnvironmentId, so it is always a permitted option.
	// Without `value`, pages that show one environment at a time pick the route's or selected one, or the first option.
	let {
		value = $bindable(),
		permission,
		environments
	}: { value?: string; permission?: string; environments?: EnvironmentRef[] } = $props();

	const selectsEnvironment = $derived(value === undefined);
	const options = $derived(
		(environments ?? environmentStore.shown)
			.filter((env) => !permission || hasPermission(permission, env.id))
			.map((env) => ({ value: env.id, label: env.name }))
	);
	// Routes under /environments/[id] show that environment, which can differ from the selected one.
	const routeEnvironmentId = $derived(page.route.id?.includes('/environments/[id]') ? page.params.id : undefined);
	const pageEnvironmentId = $derived(
		options.find((option) => option.value === (routeEnvironmentId ?? environmentStore.selected?.id))?.value ??
			options[0]?.value ??
			''
	);

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
</script>

{#if environmentStore.shown.length > 1}
	{#if selectsEnvironment}
		<SelectWithLabel
			id="page-environment"
			label={m.resource_environment_cap()}
			bind:value={() => pageEnvironmentId, selectEnvironment}
			{options}
			hideLabel
			compact
		/>
	{:else}
		<div class="max-w-sm">
			<SelectWithLabel
				id="destination-environment"
				label={m.resource_environment_cap()}
				bind:value={() => value ?? '', (next) => (value = next)}
				{options}
			/>
		</div>
	{/if}
{/if}
