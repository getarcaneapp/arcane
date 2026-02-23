<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { VolumesIcon, TerminalIcon, FolderOpenIcon } from '$lib/icons';

	interface Props {
		mounts: any[];
	}

	let { mounts }: Props = $props();

	function getMountType(mount: any): string {
		return mount.Type || mount.type || 'volume';
	}

	function getMountSource(mount: any): string {
		return mount.Source || mount.source || '';
	}

	function getMountTarget(mount: any): string {
		return mount.Target || mount.target || '';
	}

	function getMountReadOnly(mount: any): boolean {
		return mount.ReadOnly || mount.readOnly || false;
	}

	function isBindBackedVolume(mount: any): boolean {
		const opts = mount.volumeOptions;
		return opts?.type === 'none' && opts?.o === 'bind';
	}

	function getMountLabel(type: string, mount: any): string {
		if (type === 'bind') return m.containers_mount_type_bind();
		if (type === 'tmpfs') return m.containers_mount_type_tmpfs();
		return m.containers_mount_type_volume();
	}

	function getMountIconColor(type: string, mount: any): { bg: string; text: string } {
		if (type === 'bind' || (type === 'volume' && isBindBackedVolume(mount))) {
			return { bg: 'bg-blue-500/10', text: 'text-blue-500' };
		}
		if (type === 'volume') return { bg: 'bg-purple-500/10', text: 'text-purple-500' };
		return { bg: 'bg-amber-500/10', text: 'text-amber-500' };
	}
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={VolumesIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.containers_nav_storage()}</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if mounts.length > 0}
				<div class="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
					{#each mounts as mount, i (i)}
						{@const type = getMountType(mount)}
						{@const source = getMountSource(mount)}
						{@const target = getMountTarget(mount)}
						{@const readOnly = getMountReadOnly(mount)}
						{@const iconColor = getMountIconColor(type, mount)}
						{@const bindBacked = type === 'volume' && isBindBackedVolume(mount)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="border-border mb-4 flex items-center justify-between border-b pb-4">
									<div class="flex items-center gap-3">
										<div class="rounded-lg p-2 {iconColor.bg}">
											{#if type === 'volume' && !bindBacked}
												<VolumesIcon class="size-5 {iconColor.text}" />
											{:else if type === 'bind' || bindBacked}
												<FolderOpenIcon class="size-5 {iconColor.text}" />
											{:else}
												<TerminalIcon class="size-5 {iconColor.text}" />
											{/if}
										</div>
										<div class="min-w-0 flex-1">
											<div class="text-foreground text-base font-semibold break-all">
												{type === 'tmpfs' ? m.containers_mount_type_tmpfs() : source || m.image_update_auth_anonymous()}
											</div>
											<div class="mt-1 flex flex-wrap items-center gap-1.5">
												<span class="text-muted-foreground text-xs">{getMountLabel(type, mount)}</span>
												{#if mount.volumeDriver}
													<StatusBadge text={mount.volumeDriver} variant="gray" size="sm" minWidth="none" />
												{/if}
											</div>
										</div>
									</div>
									<Badge variant={readOnly ? 'secondary' : 'outline'} class="text-xs font-semibold">
										{readOnly ? m.common_ro() : m.common_rw()}
									</Badge>
								</div>

								<div class="grid grid-cols-1 gap-3">
									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">
												{m.containers_mount_label_container()}
											</div>
											<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
												{target}
											</div>
										</Card.Content>
									</Card.Root>

									{#if source && type !== 'tmpfs'}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">
													{type === 'volume' ? m.containers_mount_label_volume() : m.containers_mount_label_host()}
												</div>
												<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
													{source}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if bindBacked && mount.volumeOptions?.device}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">{m.dashboard_meter_gpu_device()}:</div>
												<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
													{mount.volumeOptions.device}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if mount.devicePath}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">
													{bindBacked ? m.containers_mount_label_volume() : m.containers_mount_label_host()}
												</div>
												<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
													{mount.devicePath}
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
					<div class="bg-muted/30 mx-auto mb-4 flex size-16 items-center justify-center rounded-full">
						<VolumesIcon class="text-muted-foreground size-6" />
					</div>
					<div class="text-muted-foreground text-sm">{m.containers_no_mounts_configured()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
