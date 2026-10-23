<script lang="ts">
	import type { Snippet } from 'svelte';

	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasAnyPermission, isGlobalAdmin } from '#lib/utils/auth.js';

	let {
		perm,
		envId,
		adminOnly = false,
		children,
		fallback
	}: {
		perm?: string | string[];
		envId?: string;
		adminOnly?: boolean;
		children: Snippet;
		fallback?: Snippet;
	} = $props();

	const allowed = $derived.by(() => {
		if (adminOnly) return isGlobalAdmin();
		if (perm === undefined) return true;
		const perms = typeof perm === 'string' ? [perm] : perm;
		const envIds = envId === undefined ? environmentStore.shown.map((env) => env.id) : [envId];
		return envIds.some((id) => hasAnyPermission(perms, id));
	});
</script>

{#if allowed}
	{@render children()}
{:else if fallback}
	{@render fallback()}
{/if}
