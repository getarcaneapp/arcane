<script lang="ts">
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { VolumesIcon, TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetailsDto } from '#lib/types/docker.js';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const mountIconBgClass: Record<string, string> = {
		volume: 'bg-primary/10',
		bind: 'bg-info/10'
	};

	function mountSourceLabel(type: string): string {
		switch (type) {
			case 'volume':
				return m.containers_mount_label_volume();
			case 'bind':
				return m.containers_mount_label_host();
			default:
				return m.containers_mount_label_source();
		}
	}
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={VolumesIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>
						{m.containers_storage_title()}
					</h2>
				</Card.Title>
				<Card.Description>{m.containers_storage_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content>
			{#if container.mounts && container.mounts.length > 0}
				<div class="divide-y divide-border/60">
					{#each container.mounts as mount (mount.destination)}
						<section class="py-5 first:pt-0 last:pb-0">
							<div class="flex items-center justify-between gap-3 pb-1">
								<div class="flex min-w-0 items-center gap-3">
									<div class={['rounded-lg p-2', mountIconBgClass[mount.type] ?? 'bg-warning/10']}>
										{#if mount.type === 'volume'}
											<VolumesIcon class="size-4 text-primary" />
										{:else if mount.type === 'bind'}
											<VolumesIcon class="size-4 text-info" />
										{:else}
											<TerminalIcon class="size-4 text-warning" />
										{/if}
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-sm font-semibold break-all text-foreground">
											{#if mount.type === 'tmpfs'}
												{m.containers_mount_type_tmpfs()}
											{:else if mount.type === 'volume'}
												{mount.name || m.containers_mount_type_volume()}
											{:else}
												{m.containers_mount_type_bind()}
											{/if}
										</div>
										<div class="text-xs text-muted-foreground">
											{mount.type} mount
										</div>
									</div>
								</div>
								<Badge variant={mount.rw ? 'outline' : 'secondary'}>
									{mount.rw ? m.common_rw() : m.common_ro()}
								</Badge>
							</div>

							<KeyValueGrid>
								<KeyValueCard label={m.containers_mount_label_container()} valueTitle={m.common_click_to_select()}>
									{mount.destination}
								</KeyValueCard>
								<KeyValueCard label={mountSourceLabel(mount.type)} valueTitle={m.common_click_to_select()}>
									{mount.source}
								</KeyValueCard>
								{#if mount.type === 'volume' && mount.driver}
									<KeyValueCard label={m.common_driver()} valueFormat="text">{mount.driver}</KeyValueCard>
								{/if}
								{#if mount.propagation}
									<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service storage; typed Mount vs ServiceMount props diverge across the boundary -->
									<KeyValueCard label={m.container_propagation()} valueFormat="text">{mount.propagation}</KeyValueCard>
								{/if}
							</KeyValueGrid>
						</section>
					{/each}
				</div>
			{:else}
				<div class="rounded-lg border border-dashed py-12 text-center">
					<div class="mx-auto mb-4 flex size-16 items-center justify-center rounded-full bg-muted/30">
						<VolumesIcon class="size-6 text-muted-foreground" />
					</div>
					<div class="text-sm text-muted-foreground">{m.containers_no_mounts_configured()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
