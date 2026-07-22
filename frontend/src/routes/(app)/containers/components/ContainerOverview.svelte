<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { formatDistanceToNow } from 'date-fns';
	import { formatDateTimeShort } from '$lib/utils/formatting';
	import { InfoIcon, StartIcon, StopIcon, NetworksIcon, VolumesIcon, HealthIcon } from '$lib/icons';
	import { containerService } from '$lib/services/container-service';
	import { KeyValueCard } from '$lib/components/resource-detail';
	import { toast } from 'svelte-sonner';

	interface Props {
		container: ContainerDetailsDto;
		primaryIpAddress: string;
		autoUpdateEnabled?: boolean;
		autoUpdateLabelControlled?: boolean;
		onAutoUpdateChange?: (enabled: boolean) => void;
		onViewPortMappings?: () => void;
	}

	let {
		container,
		primaryIpAddress,
		autoUpdateEnabled = true,
		autoUpdateLabelControlled = false,
		onAutoUpdateChange,
		onViewPortMappings
	}: Props = $props();

	let autoUpdateToggling = $state(false);

	async function handleAutoUpdateToggle(checked: boolean) {
		autoUpdateToggling = true;
		try {
			await containerService.setAutoUpdate(container.id, checked);
			onAutoUpdateChange?.(checked);
			toast.success(checked ? m.auto_update_enabled_toast() : m.auto_update_disabled_toast());
		} catch {
			toast.error(m.auto_update_failed());
		} finally {
			autoUpdateToggling = false;
		}
	}

	function parseDockerDate(input: string | Date | undefined | null): Date | null {
		if (!input) return null;
		if (input instanceof Date) return isNaN(input.getTime()) ? null : input;

		const s = String(input).trim();
		if (!s || s.startsWith('0001-01-01')) return null;

		const m = s.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?Z$/);
		let normalized = s;
		if (m) {
			const base = m[1];
			const frac = m[2] ? m[2].slice(1) : '';
			const ms = frac ? '.' + frac.slice(0, 3).padEnd(3, '0') : '';
			normalized = `${base}${ms}Z`;
		}

		const d = new Date(normalized);
		return isNaN(d.getTime()) ? null : d;
	}

	function formatDockerDate(input: string | Date | undefined | null): string {
		const d = parseDockerDate(input);
		return d ? formatDateTimeShort(d) : 'N/A';
	}

	function formatRelativeDate(input: string | Date | undefined | null): string {
		const d = parseDockerDate(input);
		if (!d) return 'N/A';
		try {
			return formatDistanceToNow(d, { addSuffix: true });
		} catch {
			return 'N/A';
		}
	}

	function getUptime(input: string | Date | undefined | null): string {
		const d = parseDockerDate(input);
		if (!d) return 'N/A';
		try {
			return formatDistanceToNow(d, { addSuffix: false });
		} catch {
			return 'N/A';
		}
	}

	const restartPolicy = $derived(container.hostConfig?.restartPolicy || 'no');

	// Deduplicate and categorize ports
	const uniquePorts = $derived.by(() => {
		if (!container.ports?.length) return { published: 0, exposed: 0, total: 0 };

		const seen = new Set<string>();
		let published = 0;
		let exposed = 0;

		for (const p of container.ports) {
			const privatePort = (p as any).privatePort ?? (p as any).target ?? 0;
			const publicPort = (p as any).publicPort ?? (p as any).hostPort ?? (p as any).published ?? null;
			const proto = (p as any).type ?? (p as any).protocol ?? 'tcp';

			// Create unique key for deduplication
			const key = `${publicPort ?? ''}:${privatePort}/${proto}`;
			if (seen.has(key)) continue;
			seen.add(key);

			if (publicPort && publicPort !== 0) {
				published++;
			} else {
				exposed++;
			}
		}

		return { published, exposed, total: published + exposed };
	});

	const mountCount = $derived(container.mounts?.length || 0);
	const networkCount = $derived(container.networkSettings?.networks ? Object.keys(container.networkSettings.networks).length : 0);
</script>

<Card.Root>
	<Card.Header icon={InfoIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>
					{m.common_details_title({ resource: m.container() })}
				</h2>
			</Card.Title>
			<Card.Description>{m.common_details_description({ resource: m.resource_container() })}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		<div class="mb-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
			<div>
				<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
					{m.common_image()}
				</div>
				<div class="flex items-center gap-3">
					<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-blue-500/10">
						<VolumesIcon class="size-5 text-blue-500" />
					</div>
					<div
						class="cursor-pointer text-base font-semibold break-all text-foreground select-all"
						title={m.common_click_to_select()}
					>
						{container.image || m.common_na()}
					</div>
				</div>
			</div>

			{#if container.state?.running}
				<div>
					<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_uptime()}</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-green-500/10">
							<StartIcon class="size-5 text-green-500" />
						</div>
						<div class="text-base font-semibold text-foreground">
							{getUptime(container.state.startedAt)}
						</div>
					</div>
				</div>
			{:else}
				<div>
					<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_status()}</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-gray-500/10">
							<StopIcon class="size-5 text-gray-500" />
						</div>
						<div class="text-base font-semibold text-foreground">
							{container.state?.status || m.common_stopped()}
						</div>
					</div>
				</div>
			{/if}

			<div>
				<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
					{m.containers_ip_address()}
				</div>
				<div class="flex items-center gap-3">
					<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-purple-500/10">
						<NetworksIcon class="size-5 text-purple-500" />
					</div>
					<div
						class="cursor-pointer font-mono text-base font-semibold text-foreground select-all"
						title={m.common_click_to_select()}
					>
						{primaryIpAddress}
					</div>
				</div>
			</div>

			{#if container.state?.health}
				<div>
					<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_health_status()}</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-pink-500/10">
							<HealthIcon class="size-5 text-pink-500" />
						</div>
						<Badge
							variant={container.state.health.status === 'healthy'
								? 'green'
								: container.state.health.status === 'unhealthy'
									? 'red'
									: 'amber'}
							minWidth="20">{container.state.health.status}</Badge
						>
					</div>
				</div>
			{/if}
		</div>

		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<KeyValueCard label={m.common_id()} valueTitle={m.common_click_to_select()}>{container.id}</KeyValueCard>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.common_created()}
					</div>
					<div class="text-sm font-medium text-foreground">
						{formatRelativeDate(container?.created)}
					</div>
					<div class="text-xs text-muted-foreground">
						{formatDockerDate(container?.created)}
					</div>
				</Card.Content>
			</Card.Root>

			{#if container.state?.running}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_started()}</div>
						<div class="text-sm font-medium text-foreground">
							{formatRelativeDate(container.state.startedAt)}
						</div>
						<div class="text-xs text-muted-foreground">
							{formatDockerDate(container.state.startedAt)}
						</div>
					</Card.Content>
				</Card.Root>
			{:else if container.state?.finishedAt && !container.state.finishedAt.startsWith('0001')}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_finished()}</div>
						<div class="text-sm font-medium text-foreground">
							{formatRelativeDate(container.state.finishedAt)}
						</div>
						<div class="text-xs text-muted-foreground">
							{formatDockerDate(container.state.finishedAt)}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_restart_policy()}</div>
					<div class="text-sm font-medium text-foreground capitalize">
						{restartPolicy}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.auto_update_title()}</div>
					<div class="flex items-center gap-3">
						<Switch
							checked={autoUpdateEnabled}
							disabled={autoUpdateToggling || autoUpdateLabelControlled}
							onCheckedChange={handleAutoUpdateToggle}
						/>
						<span class="text-sm font-medium text-foreground">
							{autoUpdateEnabled ? m.common_enabled() : m.common_disabled()}
						</span>
					</div>
					{#if autoUpdateLabelControlled}
						<span class="text-xs text-muted-foreground">{m.auto_update_controlled_by_label()}</span>
					{/if}
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_ports()}</div>
					<div class="text-sm font-medium text-foreground">
						{#if uniquePorts.total === 0}
							{m.containers_no_ports()}
						{:else if uniquePorts.published > 0 && uniquePorts.exposed > 0}
							{m.containers_ports_published_exposed({ published: uniquePorts.published, exposed: uniquePorts.exposed })}
						{:else if uniquePorts.published > 0}
							{m.containers_ports_published({ published: uniquePorts.published })}
						{:else}
							{m.containers_ports_exposed({ exposed: uniquePorts.exposed })}
						{/if}
					</div>
					{#if onViewPortMappings && uniquePorts.total > 0}
						<button type="button" class="w-fit text-xs font-medium text-primary hover:underline" onclick={onViewPortMappings}>
							{m.common_view_details()} → {m.resource_networks_cap()}
						</button>
					{/if}
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.resource_volumes_cap()}</div>
					<div class="text-sm font-medium text-foreground">
						{mountCount}
						{mountCount === 1 ? m.common_mount() : m.common_mounts()}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.resource_networks_cap()}</div>
					<div class="text-sm font-medium text-foreground">
						{networkCount}
						{networkCount === 1 ? m.resource_network() : m.resource_networks()}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_image_id()}</div>
					<div
						class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
						title={m.common_click_to_select()}
					>
						{container.imageId}
					</div>
				</Card.Content>
			</Card.Root>

			{#if container.config?.workingDir}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							{m.common_working_directory()}
						</div>
						<div
							class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{container.config.workingDir}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.user}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_user()}</div>
						<div
							class="cursor-pointer font-mono text-sm font-medium text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{container.config.user}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.entrypoint && container.config.entrypoint.length > 0}
				<Card.Root variant="subtle" class="sm:col-span-2">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_entrypoint()}</div>
						<div
							class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{container.config.entrypoint.join(' ')}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.cmd && container.config.cmd.length > 0}
				<Card.Root variant="subtle" class="sm:col-span-2 lg:col-span-3 xl:col-span-4">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							{m.common_command()}
						</div>
						<div
							class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{container.config.cmd.join(' ')}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	</Card.Content>
</Card.Root>
