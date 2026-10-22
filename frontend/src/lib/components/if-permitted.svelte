<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { Permission } from '#lib/permissions.generated.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasAnyPermission } from '#lib/utils/auth.js';

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
