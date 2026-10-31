import { m } from '#lib/paraglide/messages.js';
import type { DockerInfo } from '#lib/types/docker.js';
import type { PodmanInfo, PodmanInfoIdMap, PodmanStatus } from '#lib/types/podman.js';
import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';

type Value = string | number | null | undefined;

export type EngineRow =
	| { kind: 'info'; label: string; value: Value; mono: boolean }
	| { kind: 'tags'; label?: string; items: string[]; mono?: boolean }
	| { kind: 'block'; label: string; value: string }
	| { kind: 'commit'; value: string | undefined };

export interface EngineSection {
	title: string;
	caps?: { label: string; value: boolean | undefined }[];
	rows: EngineRow[];
}

export interface EngineView {
	warnings: string[];
	stats: { label: string; value: Value; dot?: string }[];
	sections: EngineSection[];
}

function info(label: string, value: Value, mono = true): EngineRow {
	return { kind: 'info', label, value, mono };
}

function present(label: string, value: Value, mono = true): EngineRow[] {
	return value === undefined || value === null || value === '' ? [] : [info(label, value, mono)];
}

function tags(label: string, items: string[] | undefined): EngineRow[] {
	return items && items.length > 0 ? [{ kind: 'tags', label, items }] : [];
}

function formatTime(value: string | undefined): string {
	if (!value) return '-';
	return formatDateTimeShort(value) || value;
}

function shortCommit(commit: { ID?: string } | undefined | null): string {
	return commit?.ID?.slice(0, 12) || '-';
}

function firstLine(value: string | undefined): string {
	return value?.split('\n')[0]?.trim() ?? '';
}

function formatIdMap(entries: PodmanInfoIdMap[] | undefined): string {
	return (entries ?? []).map((entry) => `${entry.container_id}:${entry.host_id}:${entry.size}`).join(', ');
}

function storageDetails(entries: string[][]): EngineSection[] {
	return entries.length > 0
		? [
				{
					title: m.docker_info_storage_details_section(),
					rows: entries.map((entry) => info(entry[0] ?? '', entry[1], false))
				}
			]
		: [];
}

export function dockerEngineView(docker: DockerInfo): EngineView {
	const swarm = docker.Swarm as {
		LocalNodeState?: string;
		ControlAvailable?: boolean;
		NodeID?: string;
		Managers?: number;
		Nodes?: number;
	};
	const swarmActive = !!swarm?.LocalNodeState && swarm.LocalNodeState !== 'inactive';

	return {
		warnings: docker.Warnings ?? [],
		stats: [
			{ label: m.common_running(), value: docker.ContainersRunning ?? 0, dot: 'bg-success' },
			{ label: m.paused(), value: docker.ContainersPaused ?? 0, dot: 'bg-warning' },
			{ label: m.common_stopped(), value: docker.ContainersStopped ?? 0, dot: 'bg-destructive' },
			{ label: m.images(), value: docker.Images ?? 0, dot: 'bg-info' },
			{ label: m.common_cpus(), value: docker.NCPU ?? 0 },
			{ label: m.docker_info_memory_label(), value: docker.MemTotal ? bytes.format(docker.MemTotal) : '-' },
			{ label: m.goroutines(), value: docker.NGoroutines ?? 0 },
			{ label: m.docker_info_file_descriptors(), value: docker.NFd ?? 0 }
		],
		sections: [
			{
				title: m.docker_info_system_section(),
				rows: [
					info(m.common_name(), docker.Name),
					info(m.common_id(), docker.ID),
					info(m.docker_info_os_label(), docker.OperatingSystem),
					info(m.docker_info_os_version_label(), docker.OSVersion),
					info(m.docker_info_os_type_label(), docker.OSType),
					info(m.common_architecture(), docker.Architecture),
					info(m.docker_info_kernel_version_label(), docker.KernelVersion),
					info(m.docker_info_system_time(), formatTime(docker.SystemTime), false),
					info(m.docker_info_root_dir(), docker.DockerRootDir),
					info(m.docker_info_index_server_label(), docker.IndexServerAddress)
				]
			},
			{
				title: m.docker_info_version_section(),
				rows: [
					info(m.docker_info_server_version_label(), docker.ServerVersion),
					info(m.docker_info_api_version_label(), docker.apiVersion),
					info(m.go_version(), docker.goVersion),
					{ kind: 'commit', value: docker.gitCommit },
					info(m.build_time(), formatTime(docker.buildTime), false),
					info(m.docker_info_experimental(), docker.ExperimentalBuild ? m.common_yes() : m.common_no(), false),
					info(m.docker_info_containerd_commit_label(), shortCommit(docker.ContainerdCommit)),
					info(m.docker_info_runc_commit_label(), shortCommit(docker.RuncCommit)),
					info(m.docker_info_init_commit_label(), shortCommit(docker.InitCommit)),
					...present(m.docker_info_product_license_label(), docker.ProductLicense, false)
				]
			},
			{
				title: m.common_configuration(),
				rows: [
					info(m.docker_info_storage_driver_label(), docker.Driver),
					info(m.docker_info_logging_driver_label(), docker.LoggingDriver),
					info(m.docker_info_cgroup_driver_label(), docker.CgroupDriver),
					info(m.docker_info_cgroup_version_label(), docker.CgroupVersion),
					info(m.isolation(), docker.Isolation),
					info(m.docker_info_init_binary(), docker.InitBinary),
					info(m.docker_info_default_runtime(), docker.DefaultRuntime),
					info(m.docker_info_debug_label(), docker.Debug ? m.common_yes() : m.common_no(), false),
					info(m.docker_info_live_restore_label(), docker.LiveRestoreEnabled ? m.common_yes() : m.common_no(), false),
					info(m.docker_info_event_listeners_label(), docker.NEventsListener ?? 0, false)
				]
			},
			{
				title: `${m.resource_networks_cap()} & ${m.docker_info_proxy_label()}`,
				rows: [
					info(m.docker_info_ipv4_forwarding(), docker.IPv4Forwarding ? m.common_enabled() : m.common_disabled(), false),
					info(m.docker_info_http_proxy(), docker.HttpProxy),
					info(m.docker_info_https_proxy(), docker.HttpsProxy),
					docker.NoProxy
						? { kind: 'block', label: m.docker_info_no_proxy(), value: docker.NoProxy }
						: info(m.docker_info_no_proxy(), docker.NoProxy),
					...(docker.DefaultAddressPools && docker.DefaultAddressPools.length > 0
						? [
								{
									kind: 'block' as const,
									label: m.docker_info_address_pools_label(),
									value: docker.DefaultAddressPools.map((pool) => `${pool.Base}/${pool.Size}`).join('  ')
								}
							]
						: [])
				]
			},
			{
				title: m.docker_info_plugins_section(),
				rows: [
					{ kind: 'tags', label: m.resource_volumes_cap(), items: docker.Plugins?.Volume ?? [] },
					{ kind: 'tags', label: m.resource_networks_cap(), items: docker.Plugins?.Network ?? [] },
					{ kind: 'tags', label: m.common_logs(), items: docker.Plugins?.Log ?? [] },
					{ kind: 'tags', label: m.docker_info_authorization_plugin(), items: docker.Plugins?.Authorization ?? [] }
				]
			},
			{
				title: m.docker_info_capabilities_section(),
				caps: [
					{ label: m.docker_info_memory_limit_label(), value: docker.MemoryLimit },
					{ label: m.docker_info_swap_limit_label(), value: docker.SwapLimit },
					{ label: m.docker_info_kernel_memory_tcp_label(), value: docker.KernelMemoryTCP },
					{ label: m.docker_info_cpu_cfs_period_label(), value: docker.CpuCfsPeriod },
					{ label: m.docker_info_cpu_cfs_quota_label(), value: docker.CpuCfsQuota },
					{ label: m.docker_info_cpu_shares_label(), value: docker.CPUShares },
					{ label: m.docker_info_cpu_set_label(), value: docker.CPUSet },
					{ label: m.docker_info_pids_limit_label(), value: docker.PidsLimit },
					{ label: m.docker_info_oom_kill_disable_label(), value: docker.OomKillDisable }
				],
				rows: []
			},
			...(swarmActive
				? [
						{
							title: m.swarm(),
							rows: [
								info(m.docker_info_swarm_state_label(), swarm.LocalNodeState, false),
								info(m.docker_info_swarm_manager_label(), swarm.ControlAvailable ? m.common_yes() : m.common_no(), false),
								info(m.docker_info_swarm_node_id_label(), swarm.NodeID),
								info(m.docker_info_swarm_managers_label(), swarm.Managers ?? 0, false),
								info(m.nodes(), swarm.Nodes ?? 0, false)
							]
						}
					]
				: []),
			{
				title: `${m.security()} & ${m.docker_info_runtimes()}`,
				rows: [
					{ kind: 'tags', label: m.docker_info_security_options(), items: docker.SecurityOptions ?? [] },
					{ kind: 'tags', label: m.docker_info_runtimes(), items: Object.keys(docker.Runtimes ?? {}) }
				]
			},
			...storageDetails(docker.DriverStatus ?? []),
			...(docker.Labels && docker.Labels.length > 0
				? [{ title: m.docker_info_labels_section(), rows: [{ kind: 'tags' as const, items: docker.Labels, mono: true }] }]
				: [])
		]
	};
}

export function podmanEngineView(podman: PodmanInfo, status: PodmanStatus): EngineView {
	const { host, store } = podman;
	const { distribution: name, variant, version, codename } = host.distribution;
	const distributionLabel = [name, variant, version === 'unknown' ? '' : version].filter(Boolean).join(' ');
	const rootlessNetworkHelper = host.rootlessNetworkCmd === 'slirp4netns' ? host.slirp4netns : host.pasta;

	return {
		warnings: [],
		stats: [
			{ label: m.common_running(), value: store.containerStore.running, dot: 'bg-success' },
			{ label: m.paused(), value: store.containerStore.paused, dot: 'bg-warning' },
			{ label: m.common_stopped(), value: store.containerStore.stopped, dot: 'bg-destructive' },
			{ label: m.podman_pods_title(), value: status.podCount, dot: 'bg-primary' },
			{ label: m.images(), value: store.imageStore.number, dot: 'bg-info' },
			{ label: m.common_cpus(), value: host.cpus },
			{ label: m.docker_info_memory_label(), value: host.memTotal ? bytes.format(host.memTotal) : '-' }
		],
		sections: [
			{
				title: m.docker_info_system_section(),
				rows: [
					...present(m.hostname(), host.hostname),
					...present(m.docker_info_os_label(), codename ? `${distributionLabel} (${codename})` : distributionLabel, false),
					...present(m.docker_info_kernel_version_label(), host.kernel),
					...present(m.common_architecture(), host.arch),
					...present(m.podman_info_swap(), host.swapTotal ? bytes.format(host.swapTotal) : null, false),
					...present(m.common_uptime(), host.uptime, false)
				]
			},
			{
				title: m.podman(),
				rows: [
					...present(m.common_version(), podman.version.version),
					...present(m.docker_info_api_version_label(), podman.version.apiVersion),
					...present(m.go_version(), podman.version.goVersion),
					...present(m.build_time(), podman.version.builtTime ? formatTime(podman.version.builtTime) : '', false),
					...(podman.version.gitCommit ? [{ kind: 'commit' as const, value: podman.version.gitCommit }] : []),
					info(m.common_mode(), host.security.rootless ? m.podman_info_rootless() : m.podman_info_rootful(), false),
					...present(m.podman_info_database_backend(), host.databaseBackend),
					...present(m.podman_info_event_logger(), host.eventLogger),
					...present(m.docker_info_logging_driver_label(), host.logDriver),
					info(m.podman_info_remote_service(), host.serviceIsRemote ? m.common_yes() : m.common_no(), false)
				]
			},
			{
				title: m.runtime(),
				rows: [
					...present(m.podman_info_oci_runtime(), firstLine(host.ociRuntime.version) || host.ociRuntime.name),
					...present(m.podman_info_conmon(), firstLine(host.conmon.version)),
					...present(m.docker_info_cgroup_driver_label(), host.cgroupManager),
					...present(m.docker_info_cgroup_version_label(), host.cgroupVersion),
					...tags(m.podman_info_cgroup_controllers(), host.cgroupControllers)
				]
			},
			{
				title: m.podman_pod_section_networking(),
				rows: [
					...present(m.podman_info_network_backend(), host.networkBackendInfo.version || host.networkBackend),
					...present(m.podman_info_dns(), host.networkBackendInfo.dns.version),
					...present(m.podman_info_default_network(), host.networkBackendInfo.defaultNetwork),
					...(host.security.rootless
						? present(m.podman_info_rootless_network(), firstLine(rootlessNetworkHelper.version) || host.rootlessNetworkCmd)
						: []),
					...tags(m.podman_info_search_registries(), podman.registries.search)
				]
			},
			{
				title: m.security(),
				caps: [
					{ label: m.podman_info_rootless(), value: host.security.rootless },
					{ label: m.podman_info_selinux(), value: host.security.selinuxEnabled },
					{ label: m.podman_info_apparmor(), value: host.security.apparmorEnabled },
					{ label: m.podman_info_seccomp(), value: host.security.seccompEnabled }
				],
				rows: [
					...present(m.podman_info_seccomp_profile(), host.security.seccompProfilePath),
					...present(m.podman_info_uid_map(), formatIdMap(host.idMappings.uidmap)),
					...present(m.podman_info_gid_map(), formatIdMap(host.idMappings.gidmap)),
					...tags(
						m.podman_info_capabilities(),
						(host.security.capabilities ?? '')
							.split(',')
							.map((capability) => capability.trim())
							.filter(Boolean)
					)
				]
			},
			{
				title: m.storage(),
				rows: [
					...present(m.docker_info_storage_driver_label(), store.graphDriverName),
					...present(m.docker_info_root_dir(), store.graphRoot),
					...present(m.podman_info_run_root(), store.runRoot),
					...present(m.podman_info_volume_path(), store.volumePath)
				]
			},
			...storageDetails(Object.entries(store.graphStatus ?? {})),
			{
				title: m.features_title(),
				caps: [
					{ label: m.podman_quadlets_title(), value: status.quadletsSupported },
					{ label: m.podman_info_quadlet_apps(), value: status.quadletAppsSupported },
					{ label: m.podman_info_systemd(), value: status.systemdAvailable }
				],
				rows: [
					...present(m.common_scope(), status.systemdScope, false),
					...present(
						m.auto_update_title(),
						status.autoUpdateMode === 'unavailable' ? m.common_unavailable() : status.autoUpdateMode,
						false
					)
				]
			},
			{
				title: m.docker_info_plugins_section(),
				rows: [
					...tags(m.resource_volumes_cap(), podman.plugins.volume),
					...tags(m.resource_networks_cap(), podman.plugins.network),
					...tags(m.common_logs(), podman.plugins.log)
				]
			}
		]
	};
}
