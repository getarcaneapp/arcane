<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { m } from '$lib/paraglide/messages';
	import { VolumesIcon, TerminalIcon } from '$lib/icons';

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
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="border-border mb-4 flex items-center justify-between border-b pb-4">
									<div class="flex items-center gap-3">
										<div
											class="rounded-lg p-2 {type === 'volume'
												? 'bg-purple-500/10'
												: type === 'bind'
													? 'bg-blue-500/10'
													: 'bg-amber-500/10'}"
										>
											{#if type === 'volume'}
												<VolumesIcon class="size-5 text-purple-500" />
											{:else if type === 'bind'}
												<VolumesIcon class="size-5 text-blue-500" />
											{:else}
												<TerminalIcon class="size-5 text-amber-500" />
											{/if}
										</div>
										<div class="min-w-0 flex-1">
											<div class="text-foreground text-base font-semibold break-all">
												{type === 'tmpfs' ? 'tmpfs' : source || '(anonymous)'}
											</div>
											<div class="text-muted-foreground text-xs">
												{type} mount
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
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
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
												<div
													class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
													title="Click to select"
												>
													{source}
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
