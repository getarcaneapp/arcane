<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { VolumesIcon, TerminalIcon, FolderOpenIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import type { SwarmServiceMount } from '$lib/types/swarm';

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
			return { bg: 'bg-blue-500/10', text: 'text-blue-500' };
		}
		if (type === 'volume') return { bg: 'bg-purple-500/10', text: 'text-purple-500' };
		return { bg: 'bg-amber-500/10', text: 'text-amber-500' };
	}
</script>

<DetailPanel>
	<DetailSectionCard icon={VolumesIcon} title={m.containers_nav_storage()}>
		{#if mounts.length > 0}
			<div class="divide-border/50 divide-y">
				{#each mounts as mount, i (i)}
					{@const type = getMountType(mount)}
					{@const source = getMountSource(mount)}
					{@const target = getMountTarget(mount)}
					{@const readOnly = getMountReadOnly(mount)}
					{@const iconColor = getMountIconColor(type, mount)}
					{@const bindBacked = type === 'volume' && isBindBackedVolume(mount)}
					<div class="space-y-4 py-4 first:pt-0 last:pb-0">
						<div class="flex items-center justify-between gap-3">
							<div class="flex min-w-0 items-center gap-2">
								{#if type === 'volume' && !bindBacked}
									<VolumesIcon class="size-4 shrink-0 {iconColor.text}" />
								{:else if type === 'bind' || bindBacked}
									<FolderOpenIcon class="size-4 shrink-0 {iconColor.text}" />
								{:else}
									<TerminalIcon class="size-4 shrink-0 {iconColor.text}" />
								{/if}
								<span class="text-foreground text-sm font-semibold break-all">
									{type === 'tmpfs' ? m.containers_mount_type_tmpfs() : source || m.image_update_auth_anonymous()}
								</span>
								<span class="text-muted-foreground text-xs">{getMountLabel(type)}</span>
								{#if mount.volumeDriver}
									<StatusBadge text={mount.volumeDriver} variant="gray" size="sm" minWidth="none" />
								{/if}
							</div>
							<Badge variant={readOnly ? 'secondary' : 'outline'} class="text-xs font-semibold">
								{readOnly ? m.common_ro() : m.common_rw()}
							</Badge>
						</div>

						<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
							<div class="sm:col-span-2">
								<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
									{m.containers_mount_label_container()}
								</div>
								<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
									{target}
								</div>
							</div>

							{#if source && type !== 'tmpfs'}
								<div class="sm:col-span-2">
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{type === 'volume' ? m.containers_mount_label_volume() : m.containers_mount_label_host()}
									</div>
									<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
										{source}
									</div>
								</div>
							{/if}

							{#if bindBacked && mount.volumeOptions?.['device']}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.dashboard_meter_gpu_device()}:
									</div>
									<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
										{mount.volumeOptions['device']}
									</div>
								</div>
							{/if}

							{#if mount.devicePath}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{bindBacked ? m.containers_mount_label_volume() : m.containers_mount_label_host()}
									</div>
									<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
										{mount.devicePath}
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
