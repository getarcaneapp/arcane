<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { hasAnyPermission } from '#lib/utils/auth.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { goto } from '$app/navigation';

	let { environmentId: targetEnvironmentId }: { environmentId?: string } = $props();
	const environmentId = $derived(targetEnvironmentId ?? environmentStore.selected?.id ?? '0');
</script>

<div class="flex flex-col items-start gap-4 p-6" role="status">
	<h1 class="text-xl font-semibold">{m.features_vulnerability_management()}</h1>
	{#if featureStore.status(environmentId) === 'ready'}
		<p>{m.features_vulnerability_disabled()}</p>
	{:else}
		<p>{m.features_unavailable()}</p>
		<ArcaneButton action="base" customLabel={m.common_retry()} onclick={() => void featureStore.refresh(environmentId)} />
	{/if}
	{#if hasAnyPermission(['settings:read', 'settings:write'], environmentId) && hasAnyPermission(['environments:list', 'environments:read'], '*')}
		<ArcaneButton
			action="base"
			customLabel={m.features_title()}
			onclick={() => goto(`/environments/${environmentId}?tab=features#features`)}
		/>
	{/if}
</div>
