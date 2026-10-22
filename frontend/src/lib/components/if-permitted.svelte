<!--
	IfPermitted — conditionally renders its children based on the current
	user's RBAC permissions in the active (or explicitly supplied) environment.

	The check is wrapped in `$derived.by`, which reads
	`environmentStore.selected?.id` as part of the computation. That makes the
	guard reactive to environment switches: when the user picks a different env,
	this component re-evaluates and shows/hides its children without a remount.

	Permission logic lives in `#lib/utils/auth` — this component is
	a thin presentational wrapper around `hasPermission` and `hasAnyPermission`.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { Permission } from '#lib/permissions.generated.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission, hasAnyPermission } from '#lib/utils/auth.js';

	let {
		perm,
		envId,
		children,
		fallback
	}: {
		perm?: Permission | Permission[];
		envId?: string;
		children: Snippet;
		fallback?: Snippet;
	} = $props();

	const allowed = $derived.by(() => {
		// Touch the selected env id so the derived re-runs when it changes.
		const activeEnvId = envId ?? environmentStore.selected?.id;

		if (perm === undefined) return true;
		if (typeof perm === 'string') return hasPermission(perm, activeEnvId);
		return hasAnyPermission(perm, activeEnvId);
	});
</script>

{#if allowed}
	{@render children()}
{:else if fallback}
	{@render fallback()}
{/if}
