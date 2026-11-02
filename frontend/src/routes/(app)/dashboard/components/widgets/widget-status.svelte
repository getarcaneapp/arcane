<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';

	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	import type { WidgetTarget } from './widget-target';

	let {
		target,
		data,
		error,
		resource,
		children
	}: {
		target: WidgetTarget;
		data: T | undefined;
		error: Error | null;
		resource: string;
		children: Snippet<[T]>;
	} = $props();
</script>

{#if target.status === 'unconfigured'}
	<p class="py-2 text-sm text-muted-foreground">{m.common_not_configured()}</p>
{:else if target.status === 'missing'}
	<p class="py-2 text-sm text-muted-foreground">{m.common_not_found_title({ resource: m.resource_environment_cap() })}</p>
{:else if target.status === 'denied'}
	<p class="py-2 text-sm text-muted-foreground">{m.common_access_denied()}</p>
{:else if target.status === 'offline'}
	<p class="py-2 text-sm text-muted-foreground">{m.common_unavailable()}</p>
{:else if error instanceof APIError && error.status === 404}
	<p class="py-2 text-sm text-muted-foreground">{m.common_not_found_title({ resource })}</p>
{:else if error}
	<div class="space-y-1 py-2">
		<p class="text-sm text-muted-foreground">{m.common_unavailable()}</p>
		<p class="text-xs whitespace-pre-line text-muted-foreground">{extractApiErrorMessage(error)}</p>
	</div>
{:else if data === undefined}
	<div class="space-y-3 py-2">
		<Skeleton class="h-4 w-3/4" />
		<Skeleton class="h-4 w-1/2" />
	</div>
{:else}
	{@render children(data)}
{/if}
