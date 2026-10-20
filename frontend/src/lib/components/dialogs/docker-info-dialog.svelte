<script module lang="ts">
	import type { DockerInfo } from '#lib/types/docker.js';
	import type { PodmanInfo, PodmanStatus } from '#lib/types/podman.js';

	export type EngineInfo =
		| { engine: 'docker'; docker: DockerInfo }
		| { engine: 'podman'; podman: PodmanInfo; status: PodmanStatus; podCount: number };
</script>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import DockerInfoBody from './docker-info-body.svelte';
	import PodmanInfoBody from './podman-info-body.svelte';

	interface Props {
		open: boolean;
		engine: EngineInfo['engine'];
		info: EngineInfo | null;
		infoPromise?: Promise<EngineInfo> | null;
		errorMessage?: string | null;
	}

	let { open = $bindable(), engine, info, infoPromise = null, errorMessage = null }: Props = $props();

	const title = $derived.by(() => {
		if (engine === 'podman') return m.podman();
		return info?.engine === 'docker' && info.docker.Name ? info.docker.Name : 'Docker Engine';
	});
</script>

<ResponsiveDialog
	{open}
	onOpenChange={(nextOpen) => (open = nextOpen)}
	title={m.docker_engine_title({ engine: title })}
	description={m.docker_info_dialog_description()}
	contentClass="sm:max-w-275"
	showCloseButton={false}
>
	{#snippet children()}
		{#if info}
			{@render dialogBody(info)}
		{:else if infoPromise}
			{#await infoPromise then resolvedInfo}
				{@render dialogBody(resolvedInfo)}
			{:catch}
				<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
					<p class="text-sm font-medium">{errorMessage ?? m.common_failed()}</p>
				</div>
			{/await}
		{:else if errorMessage}
			<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
				<p class="text-sm font-medium">{errorMessage}</p>
			</div>
		{:else}
			<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
				<Spinner class="size-5" />
				<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" onclick={() => (open = false)} customLabel={m.common_close()} />
	{/snippet}
</ResponsiveDialog>

{#snippet dialogBody(resolved: EngineInfo)}
	{#if resolved.engine === 'podman'}
		<PodmanInfoBody info={resolved.podman} status={resolved.status} podCount={resolved.podCount} />
	{:else}
		<DockerInfoBody info={resolved.docker} />
	{/if}
{/snippet}
