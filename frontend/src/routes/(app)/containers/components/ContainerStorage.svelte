<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { VolumesIcon, TerminalIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();
</script>

{#snippet mountValue(label: string, value: string, spanClass?: string)}
	<div class={spanClass}>
		<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{label}</div>
		<div
			class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all"
			title={m.common_click_to_select()}
		>
			{value}
		</div>
	</div>
{/snippet}

<DetailPanel>
	<DetailSectionCard icon={VolumesIcon} title={m.containers_storage_title()} description={m.containers_storage_description()}>
		{#if container.mounts && container.mounts.length > 0}
			<div class="divide-border/50 divide-y">
				{#each container.mounts as mount (mount.destination)}
					<div class="space-y-4 py-4 first:pt-0 last:pb-0">
						<div class="flex items-center justify-between gap-3">
							<div class="flex min-w-0 items-center gap-2">
								{#if mount.type === 'volume'}
									<VolumesIcon class="size-4 shrink-0 text-purple-500" />
								{:else if mount.type === 'bind'}
									<VolumesIcon class="size-4 shrink-0 text-blue-500" />
								{:else}
									<TerminalIcon class="size-4 shrink-0 text-amber-500" />
								{/if}
								<span class="text-foreground text-sm font-semibold break-all">
									{mount.type === 'tmpfs'
										? m.containers_mount_type_tmpfs()
										: mount.type === 'volume'
											? mount.name || m.containers_mount_type_volume()
											: m.containers_mount_type_bind()}
								</span>
								<span class="text-muted-foreground text-xs">{mount.type} mount</span>
							</div>
							<Badge variant={mount.rw ? 'outline' : 'secondary'} class="text-xs font-semibold">
								{mount.rw ? m.common_rw() : m.common_ro()}
							</Badge>
						</div>

						<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
							{@render mountValue(m.containers_mount_label_container(), mount.destination, 'sm:col-span-2')}
							{@render mountValue(
								mount.type === 'volume'
									? m.containers_mount_label_volume()
									: mount.type === 'bind'
										? m.containers_mount_label_host()
										: m.containers_mount_label_source(),
								mount.source ?? m.common_na(),
								'sm:col-span-2'
							)}
							{#if mount.type === 'volume' && mount.driver}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.container_driver()}
									</div>
									<div class="text-foreground mt-1 text-sm font-medium">{mount.driver}</div>
								</div>
							{/if}
							{#if mount.propagation}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.container_propagation()}
									</div>
									<div class="text-foreground mt-1 text-sm font-medium">
										<!-- fallow-ignore-next-line code-duplication container vs swarm-service storage; typed Mount vs ServiceMount props diverge across the boundary -->
										{mount.propagation}
									</div>
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<div class="rounded-lg border border-dashed py-12 text-center">
				<div class="bg-muted/30 mx-auto mb-4 flex size-16 items-center justify-center rounded-full">
					<VolumesIcon class="text-muted-foreground size-6" />
				</div>
				<div class="text-muted-foreground text-sm">{m.containers_no_mounts_configured()}</div>
			</div>
		{/if}
	</DetailSectionCard>
</DetailPanel>
