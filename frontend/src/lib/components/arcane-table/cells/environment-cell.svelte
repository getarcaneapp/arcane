<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { EnvironmentsIcon, RemoteEnvironmentIcon } from '#lib/icons/index.js';
	import { environmentColor } from '#lib/stores/environment-colors.store.svelte.js';
	import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { environmentName } from '#lib/utils/environments.js';

	let { value, plain = false }: { value: unknown; plain?: boolean } = $props();

	const environmentId = $derived(String(value ?? ''));
	const color = $derived(plain ? undefined : environmentColor(environmentId));
	const showDot = $derived(!!color && userStore.current?.preferences?.environmentLabelStyle === 'dot');
	const Icon = $derived(environmentId === LOCAL_DOCKER_ENVIRONMENT_ID ? EnvironmentsIcon : RemoteEnvironmentIcon);
</script>

{#if showDot}
	<ArcaneTooltip.Root>
		<!-- A plain span: the default trigger wrapper is interactive and would swallow the row click. -->
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				<span {...props} class="inline-flex size-4 items-center justify-center" aria-label={environmentName(environmentId)}>
					<span class="size-2.5 rounded-full bg-(--environment-color)" style:--environment-color={color}></span>
				</span>
			{/snippet}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>{environmentName(environmentId)}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{:else}
	<!-- Mixing toward the foreground keeps any picked color readable in light and dark themes. -->
	<span
		class={['inline-flex min-w-0 items-center gap-1.5 text-sm', color ? 'environment-colored' : 'text-muted-foreground']}
		style:--environment-color={color}
	>
		<Icon class="size-3.5 shrink-0" />
		<span class="whitespace-nowrap">{environmentName(environmentId)}</span>
	</span>
{/if}

<style>
	.environment-colored {
		color: color-mix(in srgb, var(--environment-color) 75%, var(--foreground));
	}
</style>
