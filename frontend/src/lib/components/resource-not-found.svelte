<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { AlertIcon, ArrowLeftIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	let {
		resource,
		resourceListTitle,
		backHref,
		onRetry
	}: {
		resource: string;
		resourceListTitle: string;
		backHref: string;
		onRetry: () => void;
	} = $props();
</script>

<div class="flex min-h-screen items-center justify-center">
	<div class="text-center">
		<div class="mb-6 inline-flex rounded-full bg-muted/50 p-6">
			<AlertIcon class="size-10 text-muted-foreground" />
		</div>
		<h2 class="mb-3 text-2xl font-medium">{m.common_not_found_title({ resource })}</h2>
		<p class="mb-8 max-w-md text-center text-muted-foreground">
			{m.common_not_found_description({ resource: resource.toLowerCase() })}
		</p>
		<div class="flex justify-center gap-4">
			<ArcaneButton action="base" href={backHref}>
				<ArrowLeftIcon class="size-4" />
				{m.common_back_to({ resource: resourceListTitle })}
			</ArcaneButton>
			<ArcaneButton action="refresh" onclick={onRetry}>
				{m.common_retry()}
			</ArcaneButton>
		</div>
	</div>
</div>
