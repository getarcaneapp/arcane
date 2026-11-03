<script lang="ts">
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { VolumesIcon, TerminalIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SwarmServiceMount } from '#lib/types/swarm.js';

	interface Props {
		mounts: SwarmServiceMount[];
	}

	let { mounts }: Props = $props();

	function getMountType(mount: SwarmServiceMount): string {
		return mount.type || 'volume';
	}

	function getMountSource(mount: SwarmServiceMount): string {
		return mount.source || '';
	}

	function getMountTarget(mount: SwarmServiceMount): string {
		return mount.target || '';
	}

	function getMountReadOnly(mount: SwarmServiceMount): boolean {
		return mount.readOnly || false;
	}

	function isBindBackedVolume(mount: SwarmServiceMount): boolean {
		const opts = mount.volumeOptions;
		return opts?.['type'] === 'none' && opts?.['o'] === 'bind';
	}

	function getMountLabel(type: string): string {
		if (type === 'bind') return m.containers_mount_type_bind();
		if (type === 'tmpfs') return m.containers_mount_type_tmpfs();
		return m.containers_mount_type_volume();
	}

	function getMountIconColor(type: string, mount: SwarmServiceMount): { bg: string; text: string } {
		if (type === 'bind' || (type === 'volume' && isBindBackedVolume(mount))) {
			return { bg: 'bg-info/10', text: 'text-info' };
		}
		if (type === 'volume') return { bg: 'bg-purple/10', text: 'text-purple' };
		return { bg: 'bg-warning/10', text: 'text-warning' };
	}
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={VolumesIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.storage()}</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content>
			{#if mounts.length > 0}
				<div class="divide-y divide-border/60">
					{#each mounts as mount (mount.target)}
						{@const type = getMountType(mount)}
						{@const source = getMountSource(mount)}
						{@const target = getMountTarget(mount)}
						{@const readOnly = getMountReadOnly(mount)}
						{@const iconColor = getMountIconColor(type, mount)}
						{@const bindBacked = type === 'volume' && isBindBackedVolume(mount)}
						<section class="py-5 first:pt-0 last:pb-0">
							<div>
								<div class="flex items-center justify-between gap-3 pb-1">
									<div class="flex items-center gap-3">
										<div class="rounded-lg p-2 {iconColor.bg}">
											{#if type === 'volume' && !bindBacked}
												<VolumesIcon class="size-4 {iconColor.text}" />
											{:else if type === 'bind' || bindBacked}
												<FolderOpenIcon class="size-4 {iconColor.text}" />
											{:else}
												<TerminalIcon class="size-4 {iconColor.text}" />
											{/if}
										</div>
										<div class="min-w-0 flex-1">
											<div class="text-sm font-semibold break-all text-foreground">
												{type === 'tmpfs' ? m.containers_mount_type_tmpfs() : source || m.image_update_auth_anonymous()}
											</div>
											<div class="mt-1 flex flex-wrap items-center gap-1.5">
												<span class="text-xs text-muted-foreground">{getMountLabel(type)}</span>
												{#if mount.volumeDriver}
													<Badge variant="gray" size="sm">{mount.volumeDriver}</Badge>
												{/if}
											</div>
										</div>
									</div>
									<Badge variant={readOnly ? 'secondary' : 'outline'}>
										{readOnly ? m.common_ro() : m.common_rw()}
									</Badge>
								</div>

								<KeyValueGrid>
									<KeyValueCard label={m.containers_mount_label_container()}>{target}</KeyValueCard>

									{#if source && type !== 'tmpfs'}
										<KeyValueCard label={type === 'volume' ? m.containers_mount_label_volume() : m.containers_mount_label_host()}>
											{source}
										</KeyValueCard>
									{/if}

									{#if bindBacked && mount.volumeOptions?.['device']}
										<KeyValueCard label={m.dashboard_meter_gpu_device()}>{mount.volumeOptions['device']}</KeyValueCard>
									{/if}

									{#if mount.devicePath}
										<KeyValueCard label={bindBacked ? m.containers_mount_label_volume() : m.containers_mount_label_host()}>
											{mount.devicePath}
										</KeyValueCard>
									{/if}
								</KeyValueGrid>
							</div>
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
