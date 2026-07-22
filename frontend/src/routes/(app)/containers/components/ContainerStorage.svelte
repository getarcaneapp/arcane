<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { VolumesIcon, TerminalIcon } from '$lib/icons';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();
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
		<Card.Content class="p-4">
			{#if container.mounts && container.mounts.length > 0}
				<div class="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
					{#each container.mounts as mount (mount.destination)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="mb-4 flex items-center justify-between border-b border-border pb-4">
									<div class="flex items-center gap-3">
										<div
											class="rounded-lg p-2 {mount.type === 'volume'
												? 'bg-purple-500/10'
												: mount.type === 'bind'
													? 'bg-blue-500/10'
													: 'bg-amber-500/10'}"
										>
											{#if mount.type === 'volume'}
												<VolumesIcon class="size-5 text-purple-500" />
											{:else if mount.type === 'bind'}
												<VolumesIcon class="size-5 text-blue-500" />
											{:else}
												<TerminalIcon class="size-5 text-amber-500" />
											{/if}
										</div>
										<div class="min-w-0 flex-1">
											<div class="text-base font-semibold break-all text-foreground">
												{mount.type === 'tmpfs'
													? m.containers_mount_type_tmpfs()
													: mount.type === 'volume'
														? mount.name || m.containers_mount_type_volume()
														: m.containers_mount_type_bind()}
											</div>
											<div class="text-xs text-muted-foreground">
												{mount.type} mount
											</div>
										</div>
									</div>
									<Badge variant={mount.rw ? 'outline' : 'secondary'} class="text-xs font-semibold">
										{mount.rw ? m.common_rw() : m.common_ro()}
									</Badge>
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									<Card.Root variant="outlined" class="sm:col-span-2">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">
												{m.containers_mount_label_container()}
											</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{mount.destination}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined" class="sm:col-span-2">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">
												{mount.type === 'volume'
													? m.containers_mount_label_volume()
													: mount.type === 'bind'
														? m.containers_mount_label_host()
														: m.containers_mount_label_source()}
											</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{mount.source}
											</div>
										</Card.Content>
									</Card.Root>

									{#if mount.type === 'volume' && mount.driver}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.common_driver()}</div>
												<div class="text-sm font-medium text-foreground">
													{mount.driver}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if mount.propagation}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.container_propagation()}</div>
												<div class="text-sm font-medium text-foreground">
													<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service storage; typed Mount vs ServiceMount props diverge across the boundary -->
													{mount.propagation}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}
								</div>
							</Card.Content>
						</Card.Root>
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
