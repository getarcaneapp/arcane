<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetailsDto } from '#lib/types/docker.js';
	import { formatDateTimeShort, formatElapsedTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';
	import { StartIcon, StopIcon, NetworksIcon, VolumesIcon, HealthIcon } from '#lib/icons/index.js';
	import { containerService } from '#lib/services/container-service.js';
	import { DetailMetaStrip, DetailSection, KeyValueCard, type DetailMetaItem } from '#lib/components/resource-detail/index.js';
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

	const createdInstant = $derived(parseInstant(container.created));
	const startedInstant = $derived(parseInstant(container.state?.startedAt));
	const finishedInstant = $derived(parseInstant(container.state?.finishedAt));

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

	const metaItems = $derived.by(() => {
		const items: DetailMetaItem[] = [{ icon: VolumesIcon, value: container.image || m.common_na(), mono: true }];
		if (container.state?.running) {
			items.push({ icon: StartIcon, label: m.common_uptime(), value: formatElapsedTime(startedInstant) || m.common_na() });
		} else {
			items.push({ icon: StopIcon, value: container.state?.status || m.common_stopped() });
		}
		items.push({ icon: NetworksIcon, value: primaryIpAddress, mono: true });
		return items;
	});

	const hasExecutionDetails = $derived(
		!!(
			container.config?.cmd?.length ||
			container.config?.entrypoint?.length ||
			container.config?.workingDir ||
			container.config?.user
		)
	);
</script>

<div class="space-y-6">
	<DetailMetaStrip items={metaItems}>
		{#if container.state?.health}
			<div class="flex items-center gap-1.5">
				<HealthIcon class="size-4 shrink-0 text-muted-foreground" />
				<Badge
					variant={container.state.health.status === 'healthy'
						? 'green'
						: container.state.health.status === 'unhealthy'
							? 'red'
							: 'amber'}
					minWidth="20">{container.state.health.status}</Badge
				>
			</div>
		{/if}
	</DetailMetaStrip>

	<DetailSection title={m.runtime()}>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<KeyValueCard label={m.common_id()} valueTitle={m.common_click_to_select()}>{container.id}</KeyValueCard>

			<KeyValueCard label={m.common_image_id()} valueTitle={m.common_click_to_select()}>{container.imageId}</KeyValueCard>

			<KeyValueCard label={m.common_created()} valueClass="text-sm font-medium text-foreground">
				{formatRelativeTime(createdInstant) || m.common_na()}
				<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(createdInstant) || m.common_na()}</div>
			</KeyValueCard>

			{#if container.state?.running}
				<KeyValueCard label={m.common_started()} valueClass="text-sm font-medium text-foreground">
					{formatRelativeTime(startedInstant) || m.common_na()}
					<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(startedInstant) || m.common_na()}</div>
				</KeyValueCard>
			{:else if container.state?.finishedAt && !container.state.finishedAt.startsWith('0001')}
				<KeyValueCard label={m.common_finished()} valueClass="text-sm font-medium text-foreground">
					{formatRelativeTime(finishedInstant) || m.common_na()}
					<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(finishedInstant) || m.common_na()}</div>
				</KeyValueCard>
			{/if}

			<KeyValueCard label={m.common_restart_policy()} valueClass="text-sm font-medium text-foreground capitalize">
				{restartPolicy}
			</KeyValueCard>

			<KeyValueCard label={m.auto_update_title()} valueClass="flex flex-col gap-2">
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
			</KeyValueCard>

			<KeyValueCard label={m.common_ports()} valueClass="flex flex-col gap-2 text-sm font-medium text-foreground">
				<div>
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
			</KeyValueCard>

			<KeyValueCard label={m.resource_volumes_cap()} valueClass="text-sm font-medium text-foreground">
				{mountCount}
				{mountCount === 1 ? m.common_mount() : m.common_mounts()}
			</KeyValueCard>

			<KeyValueCard label={m.resource_networks_cap()} valueClass="text-sm font-medium text-foreground">
				{networkCount}
				{networkCount === 1 ? m.resource_network() : m.resource_networks()}
			</KeyValueCard>
		</div>
	</DetailSection>

	{#if hasExecutionDetails}
		<DetailSection title={m.execution()}>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{#if container.config?.cmd && container.config.cmd.length > 0}
					<KeyValueCard
						label={m.common_command()}
						valueTitle={m.common_click_to_select()}
						cardClass="sm:col-span-2 lg:col-span-3 xl:col-span-4"
					>
						{container.config.cmd.join(' ')}
					</KeyValueCard>
				{/if}

				{#if container.config?.entrypoint && container.config.entrypoint.length > 0}
					<KeyValueCard label={m.common_entrypoint()} valueTitle={m.common_click_to_select()} cardClass="sm:col-span-2">
						{container.config.entrypoint.join(' ')}
					</KeyValueCard>
				{/if}

				{#if container.config?.workingDir}
					<KeyValueCard label={m.common_working_directory()} valueTitle={m.common_click_to_select()}>
						{container.config.workingDir}
					</KeyValueCard>
				{/if}

				{#if container.config?.user}
					<KeyValueCard label={m.common_user()} valueTitle={m.common_click_to_select()}>
						{container.config.user}
					</KeyValueCard>
				{/if}
			</div>
		</DetailSection>
	{/if}
</div>
