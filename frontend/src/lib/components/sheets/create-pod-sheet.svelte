<script lang="ts">
	import { z } from 'zod/v4';

	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import KeyValueEditor, { type KeyValueRow } from '#lib/components/form/key-value-editor.svelte';
	import PortMappingEditor, { type PortMappingRow } from '#lib/components/form/port-mapping-editor.svelte';
	import RowListEditor from '#lib/components/form/row-list-editor.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Accordion from '#lib/components/ui/accordion/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type {
		PodmanPodCreateRequest,
		PodmanPodDeviceLimit,
		PodmanPodExitPolicy,
		PodmanPodRestartPolicy
	} from '#lib/types/podman.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { parseList } from '#lib/utils/form-parsers.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	type CreatePodFormProps = {
		open: boolean;
		environments: EnvironmentRef[];
		onSubmit: (request: PodmanPodCreateRequest, environmentId: string) => void;
		isLoading: boolean;
	};

	type NetworkRow = { name: string; aliases: string; staticIps: string; staticMac: string; interfaceName: string };
	type HostRow = { host: string; ip: string };
	type DeviceRow = { device: string };
	type VolumeRow = { source: string; destination: string; options: string };
	type DeviceLimitRow = { device: string; value: string };

	let { open = $bindable(false), environments, onSubmit, isLoading }: CreatePodFormProps = $props();

	let pickedEnvironmentId = $state('');
	const environmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, PERMISSIONS.PODMAN_PODS, environments));

	const sizePattern = /^\d+(\.\d+)?\s*[bkmg]?b?$/i;
	const cpusetPattern = /^\d+(-\d+)?(,\d+(-\d+)?)*$/;
	const idMapPattern = /^\d+:\d+:\d+$/;
	const devicePattern = /^\d+:\d+$/;

	const optionalSize = z.string().refine((value) => value === '' || sizePattern.test(value), m.podman_pod_invalid_size());
	const optionalNumber = z.string().refine((value) => value === '' || Number(value) >= 0, m.podman_pod_invalid_number());
	const optionalCpuset = z.string().refine((value) => value === '' || cpusetPattern.test(value), m.podman_pod_invalid_cpuset());
	const optionalIdMap = z
		.string()
		.refine((value) => parseList(value).every((entry) => idMapPattern.test(entry)), m.podman_pod_invalid_id_map());

	const formSchema = z.object({
		podName: z
			.string()
			.min(1, m.common_field_required({ field: m.common_name() }))
			.regex(/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/, m.podman_pod_name_invalid()),
		hostname: z.string(),
		exitPolicy: z.string(),
		restartPolicy: z.string(),
		restartRetries: z.string().refine((value) => value === '' || Number.isInteger(Number(value)), m.podman_pod_invalid_number()),
		noInfra: z.boolean(),
		infraImage: z.string(),
		infraCommand: z.string(),
		infraName: z.string(),
		infraConmonPidFile: z.string(),
		sharedNamespaces: z.string(),
		shareParent: z.boolean(),
		pid: z.string(),
		ipc: z.string(),
		uts: z.string(),
		userns: z.string(),
		uidMap: optionalIdMap,
		gidMap: optionalIdMap,
		securityOpt: z.string(),
		networkMode: z.string(),
		networkPath: z.string(),
		networkOptions: z.string(),
		dnsServers: z.string(),
		dnsSearch: z.string(),
		dnsOptions: z.string(),
		noResolvConf: z.boolean(),
		noHosts: z.boolean(),
		noHostname: z.boolean(),
		hostsFile: z.string(),
		cgroupParent: z.string(),
		cpus: optionalNumber,
		cpuShares: z.string().refine((value) => value === '' || Number.isInteger(Number(value)), m.podman_pod_invalid_number()),
		cpusetCpus: optionalCpuset,
		cpusetMems: optionalCpuset,
		memory: optionalSize,
		memorySwap: z
			.string()
			.refine((value) => value === '' || value === '-1' || sizePattern.test(value), m.podman_pod_invalid_size()),
		blkioWeight: z
			.string()
			.refine(
				(value) => value === '' || (Number.isInteger(Number(value)) && Number(value) >= 10 && Number(value) <= 1000),
				m.podman_pod_invalid_blkio_weight()
			),
		gpus: z.string(),
		volumesFrom: z.string(),
		shmSize: optionalSize,
		shmSizeSystemd: optionalSize
	});

	const defaults: z.infer<typeof formSchema> = {
		podName: '',
		hostname: '',
		exitPolicy: 'default',
		restartPolicy: 'default',
		restartRetries: '',
		noInfra: false,
		infraImage: '',
		infraCommand: '',
		infraName: '',
		infraConmonPidFile: '',
		sharedNamespaces: '',
		shareParent: true,
		pid: '',
		ipc: '',
		uts: '',
		userns: '',
		uidMap: '',
		gidMap: '',
		securityOpt: '',
		networkMode: 'default',
		networkPath: '',
		networkOptions: '',
		dnsServers: '',
		dnsSearch: '',
		dnsOptions: '',
		noResolvConf: false,
		noHosts: false,
		noHostname: false,
		hostsFile: '',
		cgroupParent: '',
		cpus: '',
		cpuShares: '',
		cpusetCpus: '',
		cpusetMems: '',
		memory: '',
		memorySwap: '',
		blkioWeight: '',
		gpus: '',
		volumesFrom: '',
		shmSize: '',
		shmSizeSystemd: ''
	};

	function emptyRows() {
		return {
			labels: [] as KeyValueRow[],
			sysctls: [] as KeyValueRow[],
			ports: [] as PortMappingRow[],
			networks: [] as NetworkRow[],
			hosts: [] as HostRow[],
			devices: [] as DeviceRow[],
			volumes: [] as VolumeRow[],
			weightDevices: [] as DeviceLimitRow[],
			readBps: [] as DeviceLimitRow[],
			writeBps: [] as DeviceLimitRow[]
		};
	}

	let resetToken = $state(0);
	let submitted = $state(false);
	let rows = $state(emptyRows());
	let form = $derived.by(() => {
		resetToken;
		return createForm<typeof formSchema>(formSchema, { ...defaults });
	});
	let inputs = $derived(form.inputs);

	const exitPolicies = [
		{ value: 'default', label: m.podman_pod_restart_policy_default() },
		{ value: 'continue', label: m.podman_pod_exit_policy_continue() },
		{ value: 'stop', label: m.podman_pod_exit_policy_stop() }
	];
	const restartPolicies = [
		{ value: 'default', label: m.podman_pod_restart_policy_default() },
		{ value: 'no', label: m.common_no() },
		{ value: 'always', label: m.common_always() },
		{ value: 'on-failure', label: m.restart_policy_on_failure() },
		{ value: 'unless-stopped', label: m.restart_policy_unless_stopped() }
	];
	const networkModes = [
		{ value: 'default', label: m.podman_pod_network_mode_default() },
		{ value: 'bridge', label: m.bridge() },
		{ value: 'host', label: m.host() },
		{ value: 'none', label: m.none() },
		{ value: 'private', label: m.podman_pod_network_mode_private() },
		{ value: 'slirp4netns', label: 'slirp4netns' },
		{ value: 'pasta', label: 'pasta' },
		{ value: 'path', label: m.podman_pod_network_mode_path() }
	];

	const filled = (...values: string[]) => values.some((value) => value.trim() !== '');
	const deviceLimitInvalid = (limits: DeviceLimitRow[]) =>
		limits.some((row) => filled(row.device, row.value) && (!devicePattern.test(row.device.trim()) || !row.value.trim()));
	const portsInvalid = $derived(
		rows.ports.some((row) => filled(row.hostPort, row.containerPort, row.hostIp) && !(Number(row.containerPort) > 0))
	);
	const networksInvalid = $derived(
		rows.networks.some((row) => filled(row.aliases, row.staticIps, row.staticMac, row.interfaceName) && !row.name.trim())
	);
	const hostsInvalid = $derived(rows.hosts.some((row) => filled(row.host, row.ip) && !(row.host.trim() && row.ip.trim())));
	const volumesInvalid = $derived(
		rows.volumes.some((row) => filled(row.source, row.destination, row.options) && !(row.source.trim() && row.destination.trim()))
	);
	const limitsInvalid = $derived(
		deviceLimitInvalid(rows.weightDevices) || deviceLimitInvalid(rows.readBps) || deviceLimitInvalid(rows.writeBps)
	);

	function nonEmpty<T>(items: T[]): T[] | undefined {
		return items.length > 0 ? items : undefined;
	}

	function keyValues(entries: KeyValueRow[]): Record<string, string> | undefined {
		const pairs = entries.filter((row) => row.key.trim()).map((row) => [row.key.trim(), row.value.trim()] as const);
		return pairs.length > 0 ? Object.fromEntries(pairs) : undefined;
	}

	function deviceLimits(limits: DeviceLimitRow[]): PodmanPodDeviceLimit[] | undefined {
		return nonEmpty(
			limits.filter((row) => filled(row.device, row.value)).map((row) => ({ device: row.device.trim(), value: row.value.trim() }))
		);
	}

	function handleSubmit() {
		submitted = true;
		const data = form.validate();
		if (!data || portsInvalid || networksInvalid || hostsInvalid || volumesInvalid || limitsInvalid) return;

		const networkMode = data.networkMode === 'path' ? `ns:${data.networkPath}` : data.networkMode;
		onSubmit(
			{
				name: data.podName,
				hostname: data.hostname || undefined,
				labels: keyValues(rows.labels),
				exitPolicy: data.exitPolicy === 'default' ? undefined : (data.exitPolicy as PodmanPodExitPolicy),
				restartPolicy: data.restartPolicy === 'default' ? undefined : (data.restartPolicy as PodmanPodRestartPolicy),
				restartRetries: data.restartPolicy === 'on-failure' && data.restartRetries ? Number(data.restartRetries) : undefined,
				noInfra: data.noInfra || undefined,
				infraImage: data.noInfra ? undefined : data.infraImage || undefined,
				infraCommand: data.noInfra ? undefined : nonEmpty(data.infraCommand.split(/\s+/).filter(Boolean)),
				infraName: data.noInfra ? undefined : data.infraName || undefined,
				infraConmonPidFile: data.noInfra ? undefined : data.infraConmonPidFile || undefined,
				sharedNamespaces: nonEmpty(parseList(data.sharedNamespaces)),
				shareParent: data.shareParent ? undefined : false,
				pid: data.pid || undefined,
				ipc: data.ipc || undefined,
				uts: data.uts || undefined,
				userns: data.userns || undefined,
				uidMap: nonEmpty(parseList(data.uidMap)),
				gidMap: nonEmpty(parseList(data.gidMap)),
				securityOpt: nonEmpty(parseList(data.securityOpt)),
				sysctl: keyValues(rows.sysctls),
				networkMode: data.networkMode === 'default' ? undefined : networkMode,
				networkOptions: nonEmpty(parseList(data.networkOptions)),
				networks: nonEmpty(
					rows.networks
						.filter((row) => row.name.trim())
						.map((row) => ({
							name: row.name.trim(),
							aliases: nonEmpty(parseList(row.aliases)),
							staticIps: nonEmpty(parseList(row.staticIps)),
							staticMac: row.staticMac.trim() || undefined,
							interfaceName: row.interfaceName.trim() || undefined
						}))
				),
				portMappings: nonEmpty(
					rows.ports
						.filter((row) => row.containerPort.trim())
						.map((row) => ({
							containerPort: Number(row.containerPort),
							hostPort: row.hostPort.trim() ? Number(row.hostPort) : undefined,
							hostIp: row.hostIp.trim() || undefined,
							protocol: row.protocol
						}))
				),
				dnsServers: nonEmpty(parseList(data.dnsServers)),
				dnsSearch: nonEmpty(parseList(data.dnsSearch)),
				dnsOptions: nonEmpty(parseList(data.dnsOptions)),
				noManageResolvConf: data.noResolvConf || undefined,
				addHosts: nonEmpty(rows.hosts.filter((row) => row.host.trim()).map((row) => `${row.host.trim()}:${row.ip.trim()}`)),
				noHosts: data.noHosts || undefined,
				noHostname: data.noHostname || undefined,
				hostsFile: data.hostsFile || undefined,
				cgroupParent: data.cgroupParent || undefined,
				cpus: data.cpus ? Number(data.cpus) : undefined,
				cpuShares: data.cpuShares ? Number(data.cpuShares) : undefined,
				cpusetCpus: data.cpusetCpus || undefined,
				cpusetMems: data.cpusetMems || undefined,
				memory: data.memory || undefined,
				memorySwap: data.memorySwap || undefined,
				blkioWeight: data.blkioWeight ? Number(data.blkioWeight) : undefined,
				blkioWeightDevices: deviceLimits(rows.weightDevices),
				deviceReadBps: deviceLimits(rows.readBps),
				deviceWriteBps: deviceLimits(rows.writeBps),
				devices: nonEmpty(rows.devices.map((row) => row.device.trim()).filter(Boolean)),
				gpus: nonEmpty(parseList(data.gpus)),
				volumes: nonEmpty(
					rows.volumes
						.filter((row) => row.source.trim())
						.map((row) => ({
							source: row.source.trim(),
							destination: row.destination.trim(),
							options: nonEmpty(parseList(row.options))
						}))
				),
				volumesFrom: nonEmpty(parseList(data.volumesFrom)),
				shmSize: data.shmSize || undefined,
				shmSizeSystemd: data.shmSizeSystemd || undefined
			},
			environmentId
		);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			pickedEnvironmentId = '';
			submitted = false;
			rows = emptyRows();
			resetToken += 1;
		}
	}
</script>

{#snippet rowsError(invalid: boolean)}
	{#if submitted && invalid}
		<p class="text-xs font-medium text-destructive">{m.podman_pod_invalid_rows()}</p>
	{/if}
{/snippet}

{#snippet deviceLimitEditor(label: string, limits: DeviceLimitRow[], valuePlaceholder: string)}
	<div class="space-y-2">
		<Label>{label}</Label>
		<RowListEditor
			rows={limits}
			disabled={isLoading}
			createRow={() => ({ device: '', value: '' })}
			addLabel={m.podman_pod_add_device()}
		>
			{#snippet children(row)}
				<Input
					type="text"
					placeholder={m.podman_pod_device_limit_device_placeholder()}
					bind:value={row.device}
					disabled={isLoading}
					mono
					class="flex-1"
				/>
				<Input type="text" placeholder={valuePlaceholder} bind:value={row.value} disabled={isLoading} mono class="flex-1" />
			{/snippet}
		</RowListEditor>
		{@render rowsError(deviceLimitInvalid(limits))}
	</div>
{/snippet}

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.common_create_button({ resource: m.podman_pod() })}
	description={m.podman_pod_create_description()}
	contentClass="sm:max-w-150"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<EnvironmentDestinationSelect
				bind:value={() => environmentId, (value) => (pickedEnvironmentId = value)}
				permission={PERMISSIONS.PODMAN_PODS}
				{environments}
				variant="field"
			/>
			<FormInput
				label={m.common_name()}
				placeholder={m.podman_pod_name_placeholder()}
				disabled={isLoading}
				bind:input={inputs.podName}
			/>
			<FormInput
				label={m.hostname()}
				placeholder={m.podman_pod_hostname_placeholder()}
				disabled={isLoading}
				bind:input={inputs.hostname}
			/>

			<Accordion.Root type="single" class="w-full">
				<Accordion.Item value="networking">
					<Accordion.Trigger>{m.podman_pod_section_networking()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<SelectWithLabel
								id="pod-network-mode"
								bind:value={inputs.networkMode.value}
								label={m.podman_pod_network_mode()}
								description={m.podman_pod_network_mode_description()}
								options={networkModes}
								disabled={isLoading}
							/>
							{#if inputs.networkMode.value === 'path'}
								<FormInput
									label={m.podman_pod_network_namespace_path()}
									placeholder={m.podman_pod_network_namespace_path_placeholder()}
									disabled={isLoading}
									bind:input={inputs.networkPath}
								/>
							{/if}
							{#if inputs.networkMode.value === 'pasta' || inputs.networkMode.value === 'slirp4netns'}
								<FormInput
									label={m.podman_pod_network_options()}
									description={m.podman_pod_network_options_description()}
									placeholder={inputs.networkMode.value === 'pasta'
										? m.podman_pod_network_options_pasta_placeholder()
										: m.podman_pod_network_options_slirp_placeholder()}
									disabled={isLoading}
									bind:input={inputs.networkOptions}
								/>
							{/if}
							<div class="space-y-2">
								<Label>{m.podman_pod_networks()}</Label>
								<p class="text-xs text-muted-foreground">{m.podman_pod_networks_description()}</p>
								<RowListEditor
									bind:rows={rows.networks}
									disabled={isLoading}
									createRow={() => ({ name: '', aliases: '', staticIps: '', staticMac: '', interfaceName: '' })}
									addLabel={m.podman_pod_add_network()}
								>
									{#snippet children(row)}
										<div class="grid flex-1 grid-cols-1 gap-2 sm:grid-cols-2">
											<Input
												type="text"
												placeholder={m.podman_pod_network_name_placeholder()}
												bind:value={row.name}
												disabled={isLoading}
												mono
											/>
											<Input
												type="text"
												placeholder={m.podman_pod_network_aliases_placeholder()}
												title={m.containers_aliases()}
												bind:value={row.aliases}
												disabled={isLoading}
												mono
											/>
											<Input
												type="text"
												placeholder={m.podman_pod_network_ip_placeholder()}
												title={m.static_ip()}
												bind:value={row.staticIps}
												disabled={isLoading}
												mono
											/>
											<Input
												type="text"
												placeholder={m.podman_pod_network_mac_placeholder()}
												title={m.containers_mac_address()}
												bind:value={row.staticMac}
												disabled={isLoading}
												mono
											/>
											<Input
												type="text"
												placeholder={m.podman_pod_network_interface_placeholder()}
												title={m.podman_pod_interface_name()}
												bind:value={row.interfaceName}
												disabled={isLoading}
												mono
											/>
										</div>
									{/snippet}
								</RowListEditor>
								{@render rowsError(networksInvalid)}
							</div>
							<div class="space-y-2">
								<Label>{m.common_port_mappings()}</Label>
								<PortMappingEditor bind:rows={rows.ports} disabled={isLoading} />
								{@render rowsError(portsInvalid)}
							</div>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="dns">
					<Accordion.Trigger>{m.podman_pod_section_dns_hosts()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<FormInput
								label={m.podman_pod_dns_servers()}
								description={m.podman_comma_separated()}
								placeholder={m.podman_pod_dns_servers_placeholder()}
								disabled={isLoading}
								bind:input={inputs.dnsServers}
							/>
							<FormInput
								label={m.podman_pod_dns_search()}
								description={m.podman_comma_separated()}
								placeholder={m.podman_pod_dns_search_placeholder()}
								disabled={isLoading}
								bind:input={inputs.dnsSearch}
							/>
							<FormInput
								label={m.podman_pod_dns_options()}
								description={m.podman_comma_separated()}
								placeholder={m.podman_pod_dns_options_placeholder()}
								disabled={isLoading}
								bind:input={inputs.dnsOptions}
							/>
							<FormInput
								label={m.podman_pod_no_resolv_conf()}
								description={m.podman_pod_no_resolv_conf_description()}
								type="switch"
								disabled={isLoading}
								bind:input={inputs.noResolvConf}
							/>
							<div class="space-y-2">
								<Label>{m.podman_pod_add_hosts()}</Label>
								<p class="text-xs text-muted-foreground">{m.podman_pod_add_hosts_description()}</p>
								<RowListEditor
									bind:rows={rows.hosts}
									disabled={isLoading}
									createRow={() => ({ host: '', ip: '' })}
									addLabel={m.podman_pod_add_host()}
								>
									{#snippet children(row)}
										<Input
											type="text"
											placeholder={m.podman_pod_host_name_placeholder()}
											bind:value={row.host}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
										<Input
											type="text"
											placeholder={m.podman_pod_host_ip_placeholder()}
											bind:value={row.ip}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
									{/snippet}
								</RowListEditor>
								{@render rowsError(hostsInvalid)}
							</div>
							<FormInput
								label={m.podman_pod_hosts_file()}
								description={m.podman_pod_hosts_file_description()}
								placeholder={m.podman_pod_hosts_file_placeholder()}
								disabled={isLoading || inputs.noHosts.value}
								bind:input={inputs.hostsFile}
							/>
							<FormInput label={m.podman_pod_no_hosts()} type="switch" disabled={isLoading} bind:input={inputs.noHosts} />
							<FormInput label={m.podman_pod_no_hostname()} type="switch" disabled={isLoading} bind:input={inputs.noHostname} />
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="resources">
					<Accordion.Trigger>{m.common_resources()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.common_cpus()}
									description={m.podman_pod_cpus_description()}
									placeholder={m.podman_pod_cpus_placeholder()}
									disabled={isLoading}
									bind:input={inputs.cpus}
								/>
								<FormInput
									label={m.cpu_shares()}
									description={m.podman_pod_cpu_shares_description()}
									placeholder={m.podman_pod_cpu_shares_placeholder()}
									disabled={isLoading}
									bind:input={inputs.cpuShares}
								/>
								<FormInput
									label={m.podman_pod_cpuset_cpus()}
									description={m.podman_pod_cpuset_cpus_description()}
									placeholder={m.podman_pod_cpuset_cpus_placeholder()}
									disabled={isLoading}
									bind:input={inputs.cpusetCpus}
								/>
								<FormInput
									label={m.podman_pod_cpuset_mems()}
									description={m.podman_pod_cpuset_mems_description()}
									placeholder={m.podman_pod_cpuset_mems_placeholder()}
									disabled={isLoading}
									bind:input={inputs.cpusetMems}
								/>
								<FormInput
									label={m.podman_pod_memory()}
									description={m.podman_pod_size_description()}
									placeholder={m.podman_pod_memory_placeholder()}
									disabled={isLoading}
									bind:input={inputs.memory}
								/>
								<FormInput
									label={m.podman_pod_memory_swap()}
									description={m.podman_pod_memory_swap_description()}
									placeholder={m.podman_pod_memory_swap_placeholder()}
									disabled={isLoading}
									bind:input={inputs.memorySwap}
								/>
								<FormInput
									label={m.podman_pod_blkio_weight()}
									description={m.podman_pod_blkio_weight_description()}
									placeholder={m.podman_pod_blkio_weight_placeholder()}
									disabled={isLoading}
									bind:input={inputs.blkioWeight}
								/>
								<FormInput
									label={m.podman_pod_cgroup_parent()}
									description={m.podman_pod_cgroup_parent_description()}
									placeholder={m.podman_pod_cgroup_parent_placeholder()}
									disabled={isLoading}
									bind:input={inputs.cgroupParent}
								/>
							</div>
							<p class="text-xs text-muted-foreground">{m.podman_pod_device_limit_description()}</p>
							{@render deviceLimitEditor(
								m.podman_pod_blkio_weight_devices(),
								rows.weightDevices,
								m.podman_pod_blkio_weight_device_placeholder()
							)}
							{@render deviceLimitEditor(
								m.podman_pod_device_read_bps(),
								rows.readBps,
								m.podman_pod_device_read_bps_placeholder()
							)}
							{@render deviceLimitEditor(
								m.podman_pod_device_write_bps(),
								rows.writeBps,
								m.podman_pod_device_write_bps_placeholder()
							)}
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="storage">
					<Accordion.Trigger>{m.podman_pod_section_storage()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<div class="space-y-2">
								<Label>{m.volume_mounts_title()}</Label>
								<p class="text-xs text-muted-foreground">{m.podman_pod_volumes_description()}</p>
								<RowListEditor
									bind:rows={rows.volumes}
									disabled={isLoading}
									createRow={() => ({ source: '', destination: '', options: '' })}
									addLabel={m.add_volume_mount_button()}
								>
									{#snippet children(row)}
										<Input
											type="text"
											placeholder={m.podman_pod_volume_source_placeholder()}
											title={m.container_source_path_or_volume()}
											bind:value={row.source}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
										<Input
											type="text"
											placeholder={m.podman_pod_volume_target_placeholder()}
											title={m.container_path()}
											bind:value={row.destination}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
										<Input
											type="text"
											placeholder={m.podman_pod_volume_options_placeholder()}
											title={m.podman_pod_volume_options()}
											bind:value={row.options}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
									{/snippet}
								</RowListEditor>
								{@render rowsError(volumesInvalid)}
							</div>
							<FormInput
								label={m.podman_pod_volumes_from()}
								description={m.podman_pod_volumes_from_description()}
								placeholder={m.podman_pod_volumes_from_placeholder()}
								disabled={isLoading}
								bind:input={inputs.volumesFrom}
							/>
							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.podman_pod_shm_size()}
									description={m.podman_pod_size_description()}
									placeholder={m.podman_pod_shm_size_placeholder()}
									disabled={isLoading}
									bind:input={inputs.shmSize}
								/>
								<FormInput
									label={m.podman_pod_shm_size_systemd()}
									description={m.podman_pod_shm_size_systemd_description()}
									placeholder={m.podman_pod_shm_size_systemd_placeholder()}
									disabled={isLoading}
									bind:input={inputs.shmSizeSystemd}
								/>
							</div>
							<div class="space-y-2">
								<Label>{m.podman_pod_devices()}</Label>
								<p class="text-xs text-muted-foreground">{m.podman_pod_devices_description()}</p>
								<RowListEditor
									bind:rows={rows.devices}
									disabled={isLoading}
									createRow={() => ({ device: '' })}
									addLabel={m.podman_pod_add_device()}
								>
									{#snippet children(row)}
										<Input
											type="text"
											placeholder={m.podman_pod_device_placeholder()}
											bind:value={row.device}
											disabled={isLoading}
											mono
											class="flex-1"
										/>
									{/snippet}
								</RowListEditor>
							</div>
							<FormInput
								label={m.podman_pod_gpus()}
								description={m.podman_pod_gpus_description()}
								placeholder={m.podman_pod_gpus_placeholder()}
								disabled={isLoading}
								bind:input={inputs.gpus}
							/>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="namespaces">
					<Accordion.Trigger>{m.podman_pod_section_namespaces()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<FormInput
								label={m.podman_pod_shared_namespaces()}
								description={m.podman_pod_shared_namespaces_description()}
								placeholder={m.podman_pod_shared_namespaces_placeholder()}
								disabled={isLoading || inputs.noInfra.value}
								bind:input={inputs.sharedNamespaces}
							/>
							<FormInput
								label={m.podman_pod_share_parent()}
								description={m.podman_pod_share_parent_description()}
								type="switch"
								disabled={isLoading}
								bind:input={inputs.shareParent}
							/>
							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.podman_pod_pid_namespace()}
									description={m.podman_pod_namespace_description()}
									placeholder={m.podman_pod_pid_namespace_placeholder()}
									disabled={isLoading}
									bind:input={inputs.pid}
								/>
								<FormInput
									label={m.podman_pod_ipc_namespace()}
									description={m.podman_pod_namespace_description()}
									placeholder={m.podman_pod_ipc_namespace_placeholder()}
									disabled={isLoading}
									bind:input={inputs.ipc}
								/>
								<FormInput
									label={m.podman_pod_uts_namespace()}
									description={m.podman_pod_namespace_description()}
									placeholder={m.podman_pod_uts_namespace_placeholder()}
									disabled={isLoading}
									bind:input={inputs.uts}
								/>
								<FormInput
									label={m.podman_pod_user_namespace()}
									description={m.podman_pod_user_namespace_description()}
									placeholder={m.podman_pod_user_namespace_placeholder()}
									disabled={isLoading}
									bind:input={inputs.userns}
								/>
								<FormInput
									label={m.podman_pod_uid_map()}
									description={m.podman_pod_id_map_description()}
									placeholder={m.podman_pod_uid_map_placeholder()}
									disabled={isLoading}
									bind:input={inputs.uidMap}
								/>
								<FormInput
									label={m.podman_pod_gid_map()}
									description={m.podman_pod_id_map_description()}
									placeholder={m.podman_pod_gid_map_placeholder()}
									disabled={isLoading}
									bind:input={inputs.gidMap}
								/>
							</div>
							<FormInput
								label={m.podman_pod_security_opt()}
								description={m.podman_pod_security_opt_description()}
								placeholder={m.podman_pod_security_opt_placeholder()}
								disabled={isLoading}
								bind:input={inputs.securityOpt}
							/>
							<div class="space-y-2">
								<Label>{m.podman_pod_sysctl()}</Label>
								<p class="text-xs text-muted-foreground">{m.podman_pod_sysctl_description()}</p>
								<KeyValueEditor
									bind:rows={rows.sysctls}
									keyPlaceholder={m.podman_pod_sysctl_key_placeholder()}
									valuePlaceholder={m.podman_pod_sysctl_value_placeholder()}
									disabled={isLoading}
								/>
							</div>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="lifecycle">
					<Accordion.Trigger>{m.podman_pod_section_lifecycle()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<SelectWithLabel
								id="pod-exit-policy"
								bind:value={inputs.exitPolicy.value}
								label={m.podman_pod_exit_policy()}
								description={m.podman_pod_exit_policy_description()}
								options={exitPolicies}
								disabled={isLoading}
							/>
							<SelectWithLabel
								id="pod-restart-policy"
								bind:value={inputs.restartPolicy.value}
								label={m.restart_policy_label()}
								description={m.podman_pod_restart_policy_description()}
								options={restartPolicies}
								disabled={isLoading}
							/>
							{#if inputs.restartPolicy.value === 'on-failure'}
								<FormInput
									label={m.max_retry_label()}
									description={m.podman_pod_restart_retries_description()}
									placeholder={m.max_retry_placeholder()}
									disabled={isLoading}
									bind:input={inputs.restartRetries}
								/>
							{/if}
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="infra">
					<Accordion.Trigger>{m.podman_pod_infra()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="space-y-4 pt-4">
							<FormInput
								label={m.podman_pod_no_infra()}
								description={m.podman_pod_no_infra_description()}
								type="switch"
								disabled={isLoading}
								bind:input={inputs.noInfra}
							/>
							<FormInput
								label={m.podman_pod_infra_image()}
								description={m.podman_pod_infra_image_description()}
								placeholder={m.podman_pod_infra_image_placeholder()}
								disabled={isLoading || inputs.noInfra.value}
								bind:input={inputs.infraImage}
							/>
							<FormInput
								label={m.podman_pod_infra_command()}
								description={m.podman_pod_infra_command_description()}
								placeholder={m.podman_pod_infra_command_placeholder()}
								disabled={isLoading || inputs.noInfra.value}
								bind:input={inputs.infraCommand}
							/>
							<FormInput
								label={m.podman_pod_infra_name()}
								placeholder={m.podman_pod_infra_name_placeholder()}
								disabled={isLoading || inputs.noInfra.value}
								bind:input={inputs.infraName}
							/>
							<FormInput
								label={m.podman_pod_infra_conmon_pidfile()}
								description={m.podman_pod_infra_conmon_pidfile_description()}
								placeholder={m.podman_pod_infra_conmon_pidfile_placeholder()}
								disabled={isLoading || inputs.noInfra.value}
								bind:input={inputs.infraConmonPidFile}
							/>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="labels">
					<Accordion.Trigger>{m.common_labels()}</Accordion.Trigger>
					<Accordion.Content>
						<div class="pt-4">
							<KeyValueEditor
								bind:rows={rows.labels}
								keyPlaceholder={m.podman_pod_label_key_placeholder()}
								valuePlaceholder={m.podman_pod_label_value_placeholder()}
								addLabel={m.add_label()}
								disabled={isLoading}
							/>
						</div>
					</Accordion.Content>
				</Accordion.Item>
			</Accordion.Root>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={isLoading}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={m.common_create_button({ resource: m.podman_pod() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
