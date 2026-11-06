<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { DetailMetaStrip, DetailSection, KeyValueCard, type DetailMetaItem } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { PlayIcon, StopIcon, NetworksIcon, VolumesIcon, HealthIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainerDetails } from '#lib/types/container.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { formatDateTimeShort, formatElapsedTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface Props {
		container: ContainerDetails;
		environmentId: string;
		primaryIpAddress: string;
		autoUpdateEnabled?: boolean;
		autoUpdateLabelControlled?: boolean;
		autoUpdateStatusAvailable?: boolean;
		onAutoUpdateChange?: (enabled: boolean) => void | Promise<void>;
		onViewPortMappings?: () => void;
		onViewStorage?: () => void;
		onViewNetworks?: () => void;
	}

	let {
		container,
		environmentId,
		primaryIpAddress,
		autoUpdateEnabled = true,
		autoUpdateLabelControlled = false,
		autoUpdateStatusAvailable = true,
		onAutoUpdateChange,
		onViewPortMappings,
		onViewStorage,
		onViewNetworks
	}: Props = $props();

	let autoUpdateToggling = $state(false);

	async function handleAutoUpdateToggle(checked: boolean) {
		autoUpdateToggling = true;
		try {
			const operationResult = await tryCatch(containerService.setAutoUpdate(container.id, checked, environmentId));
			if (operationResult.error !== null) {
				toast.error(m.auto_update_failed(), { description: extractApiErrorMessage(operationResult.error) });
				return;
			}
			toast.success(checked ? m.auto_update_enabled_toast() : m.auto_update_disabled_toast());
			// The setting is saved at this point; a failed reload must not read as a failed toggle.
			const refreshResult = await tryCatch(Promise.resolve(onAutoUpdateChange?.(checked)));
			if (refreshResult.error !== null) {
				toast.error(m.common_refresh_failed({ resource: m.resource_container() }), {
					description: extractApiErrorMessage(refreshResult.error)
				});
			}
		} finally {
			autoUpdateToggling = false;
		}
	}

	const createdInstant = $derived(parseInstant(container.created));
	const startedInstant = $derived(parseInstant(container.state?.startedAt));
	const finishedInstant = $derived(parseInstant(container.state?.finishedAt));

	const restartPolicy = $derived(container.hostConfig?.restartPolicy || 'no');

	const uniquePorts = $derived.by(() => {
		if (!container.ports?.length) return { published: 0, exposed: 0, total: 0 };

		const publicPorts = Array.from(
			Map.groupBy(
				container.ports.map((p) => ({ key: `${p.publicPort ?? ''}:${p.privatePort}/${p.type}`, publicPort: p.publicPort })),
				(entry) => entry.key
			).values(),
			(group) => group[0]?.publicPort
		);
		const published = publicPorts.filter((publicPort) => publicPort && publicPort !== 0).length;

		return { published, exposed: publicPorts.length - published, total: publicPorts.length };
	});

	const mountCount = $derived(container.mounts?.length || 0);
	const networkCount = $derived(container.networkSettings?.networks ? Object.keys(container.networkSettings.networks).length : 0);

	const metaItems = $derived.by(() => {
		const items: DetailMetaItem[] = [{ icon: VolumesIcon, value: container.image || m.common_na(), mono: true }];
		if (container.state?.running) {
			items.push({ icon: PlayIcon, label: m.common_uptime(), value: formatElapsedTime(startedInstant) || m.common_na() });
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

	function getHealthStatusVariant(status: string): 'green' | 'red' | 'amber' {
		switch (status) {
			case 'healthy':
				return 'green';
			case 'unhealthy':
				return 'red';
			default:
				return 'amber';
		}
	}
</script>

<div class="space-y-6">
	<DetailMetaStrip items={metaItems}>
		{#if container.state?.health}
			<div class="flex items-center gap-1.5">
				<HealthIcon class="size-4 shrink-0 text-muted-foreground" />
				<Badge variant={getHealthStatusVariant(container.state.health.status)} minWidth="20"
					>{container.state.health.status}</Badge
				>
			</div>
		{/if}
	</DetailMetaStrip>

	<DetailSection title={m.runtime()}>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<KeyValueCard label={m.common_id()} valueTitle={m.common_click_to_select()}>{container.id}</KeyValueCard>

			<KeyValueCard label={m.common_image_id()} valueTitle={m.common_click_to_select()}>{container.imageId}</KeyValueCard>

			<KeyValueCard label={m.common_created()} valueFormat="text">
				{formatRelativeTime(createdInstant) || m.common_na()}
				<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(createdInstant) || m.common_na()}</div>
			</KeyValueCard>

			{#if container.state?.running}
				<KeyValueCard label={m.common_started()} valueFormat="text">
					{formatRelativeTime(startedInstant) || m.common_na()}
					<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(startedInstant) || m.common_na()}</div>
				</KeyValueCard>
			{:else if container.state?.finishedAt && !container.state.finishedAt.startsWith('0001')}
				<KeyValueCard label={m.common_finished()} valueFormat="text">
					{formatRelativeTime(finishedInstant) || m.common_na()}
					<div class="text-xs font-normal text-muted-foreground">{formatDateTimeShort(finishedInstant) || m.common_na()}</div>
				</KeyValueCard>
			{/if}

			<KeyValueCard label={m.common_restart_policy()} valueFormat="text">
				<span class="capitalize">{restartPolicy}</span>
			</KeyValueCard>

			<KeyValueCard label={m.auto_update_title()} stacked>
				<div class="flex items-center gap-3">
					<Switch
						checked={autoUpdateEnabled}
						disabled={autoUpdateToggling || autoUpdateLabelControlled || !autoUpdateStatusAvailable}
						onCheckedChange={handleAutoUpdateToggle}
					/>
					<span class="text-sm font-medium text-foreground">
						{#if !autoUpdateStatusAvailable}
							{m.common_na()}
						{:else if autoUpdateEnabled}
							{m.common_enabled()}
						{:else}
							{m.common_disabled()}
						{/if}
					</span>
				</div>
				{#if !autoUpdateStatusAvailable}
					<span class="text-xs text-muted-foreground">{m.auto_update_status_unavailable()}</span>
				{:else}
					{#if autoUpdateLabelControlled}
						<span class="text-xs text-muted-foreground">{m.auto_update_controlled_by_label()}</span>
					{/if}
					{#if !autoUpdateEnabled}
						<span class="text-xs text-muted-foreground">{m.auto_update_disabled_checks_continue()}</span>
					{/if}
				{/if}
			</KeyValueCard>

			<KeyValueCard label={m.common_ports()} valueFormat="text" stacked>
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
					<button type="button" class="w-fit text-xs font-medium text-primary-text hover:underline" onclick={onViewPortMappings}>
						{m.common_view_details()} → {m.resource_networks_cap()}
					</button>
				{/if}
			</KeyValueCard>

			<KeyValueCard label={m.resource_volumes_cap()} valueFormat="text">
				{#if onViewStorage}
					<Button variant="link" size="inline" class="w-fit justify-start" onclick={onViewStorage}>
						{mountCount}
						{mountCount === 1 ? m.common_mount() : m.common_mounts()}
					</Button>
				{:else}
					{mountCount}
					{mountCount === 1 ? m.common_mount() : m.common_mounts()}
				{/if}
			</KeyValueCard>

			<KeyValueCard label={m.resource_networks_cap()} valueFormat="text">
				{#if onViewNetworks}
					<Button variant="link" size="inline" class="w-fit justify-start" onclick={onViewNetworks}>
						{networkCount}
						{networkCount === 1 ? m.resource_network() : m.resource_networks()}
					</Button>
				{:else}
					{networkCount}
					{networkCount === 1 ? m.resource_network() : m.resource_networks()}
				{/if}
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
						class="sm:col-span-2 lg:col-span-3 xl:col-span-4"
					>
						{container.config.cmd.join(' ')}
					</KeyValueCard>
				{/if}

				{#if container.config?.entrypoint && container.config.entrypoint.length > 0}
					<KeyValueCard label={m.common_entrypoint()} valueTitle={m.common_click_to_select()} class="sm:col-span-2">
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
