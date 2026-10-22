<script lang="ts">
	import { goto } from '$app/navigation';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { hasAnyPermission } from '#lib/utils/auth.js';

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
	{#if hasAnyPermission([PERMISSIONS.SETTINGS_READ, PERMISSIONS.SETTINGS_WRITE], environmentId) && hasAnyPermission([PERMISSIONS.ENVIRONMENTS_LIST, PERMISSIONS.ENVIRONMENTS_READ], '*')}
		<ArcaneButton
			action="base"
			customLabel={m.features_title()}
			onclick={() => goto(`/environments/${environmentId}?tab=features#features`)}
		/>
	{/if}
</div>
