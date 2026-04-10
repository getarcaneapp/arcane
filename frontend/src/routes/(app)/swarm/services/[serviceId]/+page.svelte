<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { invalidateAll, goto } from '$app/navigation';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import { type TabItem } from '$lib/components/tab-bar/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { swarmService } from '$lib/services/swarm-service';
	import type {
		RawServiceNetworkAttachment,
		RawServiceVirtualIP,
		RawSwarmServiceMount,
		RawSwarmServicePort,
		ServiceNetworkAttachment,
		ServiceNetworkDetail,
		ServiceVirtualIP,
		SwarmServiceInspect,
		SwarmServiceMount,
		SwarmServicePort
	} from '$lib/types/swarm.type';
	import ServiceEditorDialog from '../service-editor-dialog.svelte';
	import ServiceOverview from '../components/ServiceOverview.svelte';
	import ServiceLogsPanel from '../components/ServiceLogsPanel.svelte';
	import ServiceTasksPanel from '../components/ServiceTasksPanel.svelte';
	import ServiceConfiguration from '../components/ServiceConfiguration.svelte';
	import ServiceNetwork from '../components/ServiceNetwork.svelte';
	import ServiceStorage from '../components/ServiceStorage.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import {
		ArrowLeftIcon,
		AlertIcon,
		DockIcon,
		FileTextIcon,
		JobsIcon,
		SettingsIcon,
		NetworksIcon,
		VolumesIcon,
		EditIcon,
		RedeployIcon,
		TrashIcon
	} from '$lib/icons';

	let { data } = $props();
	let service = $derived(data?.service as SwarmServiceInspect);

	let userSelectedTab = $state<string>('overview');
	let userScaleReplicas = $state<number | null>(null);
	let userScaleReplicasServiceId = $state<string | null>(null);
	let isRefreshing = $state(false);
	let isLoading = $state({ update: false, rollback: false, remove: false, scale: false });

	// Editor state
	let editOpen = $state(false);

	type ServiceContainerSpecShape = {
		Image?: string;
		Env?: string[];
		Mounts?: RawSwarmServiceMount[];
		Command?: string[];
		Args?: string[];
		Dir?: string;
		User?: string;
		Hostname?: string;
	};

	type ServiceSpecShape = {
		Name?: string;
		Labels?: Record<string, string>;
		Mode?: {
			Replicated?: { Replicas?: number };
			Global?: unknown;
		};
		TaskTemplate?: {
			ContainerSpec?: ServiceContainerSpecShape;
			Networks?: RawServiceNetworkAttachment[];
		};
	};

	// Parse spec fields
	const spec = $derived(service?.spec as ServiceSpecShape | undefined);
	const containerSpec = $derived(spec?.TaskTemplate?.ContainerSpec);
	const serviceName = $derived(spec?.Name || '');
	const serviceImage = $derived(containerSpec?.Image || '');

	const serviceMode = $derived.by(() => {
		if (spec?.Mode?.Replicated) return 'replicated';
		if (spec?.Mode?.Global !== undefined) return 'global';
		return 'unknown';
	});

	const desiredReplicas = $derived.by(() => {
		if (serviceMode === 'global') return 0;
		return spec?.Mode?.Replicated?.Replicas ?? 1;
	});
	const scaleReplicas = $derived.by(() => {
		if (userScaleReplicasServiceId === service?.id && userScaleReplicas !== null) return userScaleReplicas;
		return desiredReplicas;
	});

	const envVars = $derived(containerSpec?.Env || []);
	const labels = $derived(spec?.Labels || {});

	function normalizeMount(mount: RawSwarmServiceMount): SwarmServiceMount {
		return {
			type: mount.type ?? mount.Type ?? 'volume',
			source: mount.source ?? mount.Source ?? '',
			target: mount.target ?? mount.Target ?? '',
			readOnly: mount.readOnly ?? mount.ReadOnly ?? false,
			volumeDriver: mount.volumeDriver ?? mount.VolumeDriver,
			volumeOptions: mount.volumeOptions ?? mount.VolumeOptions,
			devicePath: mount.devicePath ?? mount.DevicePath
		};
	}

	function normalizePort(port: RawSwarmServicePort): SwarmServicePort {
		return {
			protocol: port.protocol ?? port.Protocol ?? 'tcp',
			targetPort: port.targetPort ?? port.TargetPort ?? 0,
			publishedPort: port.publishedPort ?? port.PublishedPort ?? undefined,
			publishMode: port.publishMode ?? port.PublishMode ?? undefined
		};
	}

	function normalizeNetworkAttachment(network: RawServiceNetworkAttachment): ServiceNetworkAttachment {
		return {
			target: network.target ?? network.Target ?? '',
			aliases: network.aliases ?? network.Aliases ?? []
		};
	}

	function normalizeVirtualIP(vip: RawServiceVirtualIP): ServiceVirtualIP {
		return {
			networkID: vip.networkID ?? vip.NetworkID ?? '',
			addr: vip.addr ?? vip.Addr ?? ''
		};
	}

	const mounts = $derived<SwarmServiceMount[]>(service?.mounts ?? (containerSpec?.Mounts ?? []).map(normalizeMount));
	const command = $derived(containerSpec?.Command || []);
	const args = $derived(containerSpec?.Args || []);
	const workingDir = $derived(containerSpec?.Dir || '');
	const user = $derived(containerSpec?.User || '');
	const hostname = $derived(containerSpec?.Hostname || '');

	const endpointPorts = $derived<SwarmServicePort[]>(
		((service?.endpoint?.Ports as RawSwarmServicePort[] | undefined) ?? []).map(normalizePort)
	);
	const specNetworks = $derived<ServiceNetworkAttachment[]>((spec?.TaskTemplate?.Networks ?? []).map(normalizeNetworkAttachment));
	const virtualIPs = $derived<ServiceVirtualIP[]>(
		((service?.endpoint?.VirtualIPs as RawServiceVirtualIP[] | undefined) ?? []).map(normalizeVirtualIP)
	);
	const networkDetails = $derived<Record<string, ServiceNetworkDetail>>(service?.networkDetails ?? {});

	const hasEnvVars = $derived(envVars.length > 0);
	const hasLabels = $derived(Object.keys(labels).length > 0);
	const hasAdvancedConfig = $derived(command.length > 0 || args.length > 0 || !!workingDir || !!user || !!hostname);
	const showConfiguration = $derived(hasEnvVars || hasLabels || hasAdvancedConfig);
	const hasPorts = $derived(endpointPorts.length > 0);
	const hasNetworks = $derived(specNetworks.length > 0 || virtualIPs.length > 0);
	const showNetworkTab = $derived(hasPorts || hasNetworks);
	const hasMounts = $derived(mounts.length > 0);

	const tabItems = $derived<TabItem[]>([
		{ value: 'overview', label: m.common_overview(), icon: DockIcon },
		{ value: 'logs', label: m.common_logs(), icon: FileTextIcon },
		{ value: 'tasks', label: m.swarm_tasks_title(), icon: JobsIcon },
		...(showConfiguration ? [{ value: 'config', label: m.common_configuration(), icon: SettingsIcon }] : []),
		...(showNetworkTab ? [{ value: 'network', label: m.containers_nav_networks(), icon: NetworksIcon }] : []),
		...(hasMounts ? [{ value: 'storage', label: m.containers_nav_storage(), icon: VolumesIcon }] : [])
	]);

	const selectedTab = $derived.by(() => {
		if (tabItems.some((tab) => tab.value === userSelectedTab)) return userSelectedTab;
		return tabItems[0]?.value ?? 'overview';
	});

	function onTabChange(value: string) {
		userSelectedTab = value;
	}

	function onScaleReplicasInput(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		userScaleReplicasServiceId = service?.id ?? null;
		userScaleReplicas = Number(input.value);
	}

	async function refreshData() {
		isRefreshing = true;
		await invalidateAll();
		setTimeout(() => {
			isRefreshing = false;
		}, 500);
	}

	// Editor
	const editVersion = $derived(service?.version?.index ?? service?.version?.Index ?? 0);
	const editSpec = $derived(JSON.stringify(spec ?? {}, null, 2));

	function openEdit() {
		editOpen = true;
	}

	async function handleUpdate(payload: { spec: Record<string, unknown>; options?: Record<string, unknown> }) {
		if (!service?.id) return;
		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.updateService(service.id, { version: editVersion, ...payload })),
			message: m.common_update_failed({ resource: `${m.swarm_service()} "${serviceName}"` }),
			setLoadingState: (v) => (isLoading.update = v),
			onSuccess: async () => {
				toast.success(m.common_update_success({ resource: `${m.swarm_service()} "${serviceName}"` }));
				editOpen = false;
				await refreshData();
			}
		});
	}

	function handleRollback() {
		openConfirmDialog({
			title: m.swarm_service_rollback_title(),
			message: m.swarm_service_rollback_confirm({ name: serviceName }),
			confirm: {
				label: m.swarm_service_rollback(),
				destructive: false,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.rollbackService(service.id)),
						message: m.swarm_service_rollback_failed({ name: serviceName }),
						setLoadingState: (v) => (isLoading.rollback = v),
						onSuccess: async () => {
							toast.success(m.swarm_service_rollback_success({ name: serviceName }));
							await refreshData();
						}
					});
				}
			}
		});
	}

	function handleDelete() {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_service() }),
			message: m.common_delete_confirm({ resource: m.swarm_service() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeService(service.id)),
						message: m.common_delete_failed({ resource: `${m.swarm_service()} "${serviceName}"` }),
						setLoadingState: (v) => (isLoading.remove = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_service()} "${serviceName}"` }));
							goto('/swarm/services');
						}
					});
				}
			}
		});
	}

	async function handleScale() {
		if (!service?.id || serviceMode !== 'replicated') return;
		const replicas = Math.max(0, Number(scaleReplicas) || 0);
		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.scaleService(service.id, { replicas })),
			message: m.common_update_failed({ resource: `${m.swarm_service()} "${serviceName}"` }),
			setLoadingState: (v) => (isLoading.scale = v),
			onSuccess: async () => {
				toast.success(m.swarm_service_scale_success({ name: serviceName, replicas }));
				await refreshData();
			}
		});
	}
</script>

{#if service}
	<TabbedPageLayout backUrl="/swarm/services" backLabel={m.common_back()} {tabItems} {selectedTab} {onTabChange}>
		{#snippet headerInfo()}
			<div class="flex items-center gap-2">
				<div class="bg-primary/10 flex size-9 items-center justify-center rounded-full">
					<DockIcon class="text-primary size-5" />
				</div>
				<h1 class="max-w-[300px] truncate text-lg font-semibold" title={serviceName}>
					{serviceName}
				</h1>
				<StatusBadge
					variant={serviceMode === 'replicated' ? 'blue' : serviceMode === 'global' ? 'green' : 'gray'}
					text={serviceMode}
				/>
				{#if serviceMode === 'replicated'}
					<span class="text-muted-foreground font-mono text-sm">
						{desiredReplicas}
						{m.swarm_replicas()}
					</span>
				{/if}
			</div>
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#if serviceMode === 'replicated'}
					<div class="flex items-center gap-2">
						<Input
							type="number"
							min="0"
							step="1"
							value={scaleReplicas}
							oninput={onScaleReplicasInput}
							class="h-8 w-20"
							disabled={isLoading.scale}
						/>
						<ArcaneButton action="base" tone="outline" size="sm" onclick={handleScale} disabled={isLoading.scale}>
							{m.swarm_service_scale()}
						</ArcaneButton>
					</div>
				{/if}
				<ArcaneButton action="base" tone="outline" size="sm" onclick={openEdit} disabled={isLoading.update}>
					<EditIcon class="size-4" />
					<span class="hidden sm:inline">{m.common_edit()}</span>
				</ArcaneButton>
				<ArcaneButton action="base" tone="outline" size="sm" onclick={handleRollback} disabled={isLoading.rollback}>
					<RedeployIcon class="size-4" />
					<span class="hidden sm:inline">{m.swarm_service_rollback()}</span>
				</ArcaneButton>
				<ArcaneButton action="base" tone="outline-destructive" size="sm" onclick={handleDelete} disabled={isLoading.remove}>
					<TrashIcon class="size-4" />
					<span class="hidden sm:inline">{m.common_delete()}</span>
				</ArcaneButton>
			</div>
		{/snippet}

		{#snippet tabContent(activeTab)}
			<Tabs.Content value="overview" class="h-full">
				<ServiceOverview {service} {serviceName} {serviceImage} {serviceMode} {desiredReplicas} {labels} />
			</Tabs.Content>

			<Tabs.Content value="logs" class="h-full">
				{#if selectedTab === 'logs'}
					<ServiceLogsPanel serviceId={service.id} />
				{/if}
			</Tabs.Content>

			<Tabs.Content value="tasks" class="h-full">
				{#if selectedTab === 'tasks'}
					<ServiceTasksPanel {serviceName} serviceId={service.id} />
				{/if}
			</Tabs.Content>

			{#if showConfiguration}
				<Tabs.Content value="config" class="h-full">
					<ServiceConfiguration
						{envVars}
						{labels}
						{command}
						{args}
						{workingDir}
						{user}
						{hostname}
						{hasEnvVars}
						{hasLabels}
						{hasAdvancedConfig}
					/>
				</Tabs.Content>
			{/if}

			{#if showNetworkTab}
				<Tabs.Content value="network" class="h-full">
					<ServiceNetwork ports={endpointPorts} networks={specNetworks} {virtualIPs} {networkDetails} />
				</Tabs.Content>
			{/if}

			{#if hasMounts}
				<Tabs.Content value="storage" class="h-full">
					<ServiceStorage {mounts} />
				</Tabs.Content>
			{/if}
		{/snippet}
	</TabbedPageLayout>

	{#if editOpen}
		<ServiceEditorDialog
			bind:open={editOpen}
			title={`${m.common_edit()} ${m.swarm_service()}`}
			description={m.common_edit_description()}
			submitLabel={m.common_save()}
			initialSpec={editSpec}
			isLoading={isLoading.update}
			onSubmit={handleUpdate}
		/>
	{/if}
{:else}
	<div class="flex min-h-screen items-center justify-center">
		<div class="text-center">
			<div class="bg-muted/50 mb-6 inline-flex rounded-full p-6">
				<AlertIcon class="text-muted-foreground size-10" />
			</div>
			<h2 class="mb-3 text-2xl font-medium">
				{m.common_not_found_title({ resource: m.swarm_service() })}
			</h2>
			<p class="text-muted-foreground mb-8 max-w-md text-center">
				{m.common_not_found_description({ resource: m.swarm_service().toLowerCase() })}
			</p>
			<div class="flex justify-center gap-4">
				<ArcaneButton action="base" href="/swarm/services">
					<ArrowLeftIcon class="size-4" />
					{m.common_back_to({ resource: m.swarm_services_title() })}
				</ArcaneButton>
				<ArcaneButton action="refresh" onclick={refreshData}>
					{m.common_retry()}
				</ArcaneButton>
			</div>
		</div>
	</div>
{/if}
