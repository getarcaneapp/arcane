<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { DockerInfo } from '#lib/types/docker.js';
	import { bytes, withOccurrenceKeys } from '#lib/utils/formatting.js';

	import {
		blockRow,
		capRow,
		commitRow,
		formatTime,
		infoRow,
		sectionHeading,
		stat,
		storageDetailsSection,
		tagGroup
	} from './engine-info-parts.svelte';

	let { info }: { info: DockerInfo } = $props();

	function shortCommit(commit: { ID?: string } | undefined | null): string {
		const id = commit?.ID;
		if (!id) return '-';
		return id.length > 12 ? id.slice(0, 12) : id;
	}

	const swarm = $derived(
		info.Swarm as {
			LocalNodeState?: string;
			ControlAvailable?: boolean;
			NodeID?: string;
			Managers?: number;
			Nodes?: number;
		}
	);
	const swarmActive = $derived(!!swarm?.LocalNodeState && swarm.LocalNodeState !== 'inactive');
</script>

<div class="space-y-3 pt-2">
	{#if info.Warnings && info.Warnings.length > 0}
		<div class="space-y-0.5 border-l-2 border-warning/50 pl-2.5">
			<h3 class="text-3xs font-semibold tracking-wider text-warning uppercase">
				{m.docker_info_warnings_section()}
			</h3>
			<ul class="space-y-0.5">
				{#each withOccurrenceKeys(info.Warnings) as { value: warning, key } (key)}
					<li class="text-xs wrap-anywhere text-warning">{warning}</li>
				{/each}
			</ul>
		</div>
	{/if}

	<div class="flex flex-wrap items-center gap-x-5 gap-y-1.5 border-b border-border/50 pb-2.5">
		{@render stat(m.common_running(), info.ContainersRunning ?? 0, 'bg-success')}
		{@render stat(m.paused(), info.ContainersPaused ?? 0, 'bg-warning')}
		{@render stat(m.common_stopped(), info.ContainersStopped ?? 0, 'bg-destructive')}
		{@render stat(m.images(), info.Images ?? 0, 'bg-info')}
		<div class="hidden h-3 w-px bg-border/50 sm:block"></div>
		{@render stat(m.common_cpus(), info.NCPU ?? 0)}
		{@render stat(m.docker_info_memory_label(), (info.MemTotal ? bytes.format(info.MemTotal) : null) ?? '-')}
		{@render stat(m.goroutines(), info.NGoroutines ?? 0)}
		{@render stat(m.docker_info_file_descriptors(), info.NFd ?? 0)}
	</div>

	<div class="columns-1 gap-6 sm:columns-2 lg:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid">
		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_system_section())}
			<div class="space-y-0.5">
				{@render infoRow(m.common_name(), info.Name)}
				{@render infoRow(m.common_id(), info.ID, true)}
				{@render infoRow(m.docker_info_os_label(), info.OperatingSystem)}
				{@render infoRow(m.docker_info_os_version_label(), info.OSVersion)}
				{@render infoRow(m.docker_info_os_type_label(), info.OSType)}
				{@render infoRow(m.common_architecture(), info.Architecture)}
				{@render infoRow(m.docker_info_kernel_version_label(), info.KernelVersion)}
				{@render infoRow(m.docker_info_system_time(), formatTime(info.SystemTime), false)}
				{@render infoRow(m.docker_info_root_dir(), info.DockerRootDir, true)}
				{@render infoRow(m.docker_info_index_server_label(), info.IndexServerAddress, true)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_version_section())}
			<div class="space-y-0.5">
				{@render infoRow(m.docker_info_server_version_label(), info.ServerVersion)}
				{@render infoRow(m.docker_info_api_version_label(), info.apiVersion)}
				{@render infoRow(m.go_version(), info.goVersion)}
				{@render commitRow(info.gitCommit)}
				{@render infoRow(m.build_time(), formatTime(info.buildTime), false)}
				{@render infoRow(m.docker_info_experimental(), info.ExperimentalBuild ? m.common_yes() : m.common_no(), false)}
				{@render infoRow(m.docker_info_containerd_commit_label(), shortCommit(info.ContainerdCommit), true)}
				{@render infoRow(m.docker_info_runc_commit_label(), shortCommit(info.RuncCommit), true)}
				{@render infoRow(m.docker_info_init_commit_label(), shortCommit(info.InitCommit), true)}
				{#if info.ProductLicense}
					{@render infoRow(m.docker_info_product_license_label(), info.ProductLicense, false)}
				{/if}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.common_configuration())}
			<div class="space-y-0.5">
				{@render infoRow(m.docker_info_storage_driver_label(), info.Driver)}
				{@render infoRow(m.docker_info_logging_driver_label(), info.LoggingDriver)}
				{@render infoRow(m.docker_info_cgroup_driver_label(), info.CgroupDriver)}
				{@render infoRow(m.docker_info_cgroup_version_label(), info.CgroupVersion)}
				{@render infoRow(m.isolation(), info.Isolation)}
				{@render infoRow(m.docker_info_init_binary(), info.InitBinary)}
				{@render infoRow(m.docker_info_default_runtime(), info.DefaultRuntime)}
				{@render infoRow(m.docker_info_debug_label(), info.Debug ? m.common_yes() : m.common_no(), false)}
				{@render infoRow(m.docker_info_live_restore_label(), info.LiveRestoreEnabled ? m.common_yes() : m.common_no(), false)}
				{@render infoRow(m.docker_info_event_listeners_label(), info.NEventsListener ?? 0, false)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(`${m.resource_networks_cap()} & ${m.docker_info_proxy_label()}`)}
			<div class="space-y-0.5">
				{@render infoRow(m.docker_info_ipv4_forwarding(), info.IPv4Forwarding ? m.common_enabled() : m.common_disabled(), false)}
				{@render infoRow(m.docker_info_http_proxy(), info.HttpProxy)}
				{@render infoRow(m.docker_info_https_proxy(), info.HttpsProxy)}
				{#if info.NoProxy}
					{@render blockRow(m.docker_info_no_proxy(), info.NoProxy)}
				{:else}
					{@render infoRow(m.docker_info_no_proxy(), info.NoProxy)}
				{/if}
				{#if info.DefaultAddressPools && info.DefaultAddressPools.length > 0}
					{@render blockRow(
						m.docker_info_address_pools_label(),
						info.DefaultAddressPools.map((pool) => `${pool.Base}/${pool.Size}`).join('  ')
					)}
				{/if}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_plugins_section())}
			<div class="space-y-1">
				{@render tagGroup(m.resource_volumes_cap(), info.Plugins?.Volume)}
				{@render tagGroup(m.resource_networks_cap(), info.Plugins?.Network)}
				{@render tagGroup(m.common_logs(), info.Plugins?.Log)}
				{@render tagGroup(m.docker_info_authorization_plugin(), info.Plugins?.Authorization)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_capabilities_section())}
			<div class="grid grid-cols-2 gap-x-4 gap-y-0.5">
				{@render capRow(m.docker_info_memory_limit_label(), info.MemoryLimit)}
				{@render capRow(m.docker_info_swap_limit_label(), info.SwapLimit)}
				{@render capRow(m.docker_info_kernel_memory_tcp_label(), info.KernelMemoryTCP)}
				{@render capRow(m.docker_info_cpu_cfs_period_label(), info.CpuCfsPeriod)}
				{@render capRow(m.docker_info_cpu_cfs_quota_label(), info.CpuCfsQuota)}
				{@render capRow(m.docker_info_cpu_shares_label(), info.CPUShares)}
				{@render capRow(m.docker_info_cpu_set_label(), info.CPUSet)}
				{@render capRow(m.docker_info_pids_limit_label(), info.PidsLimit)}
				{@render capRow(m.docker_info_oom_kill_disable_label(), info.OomKillDisable)}
			</div>
		</section>

		{#if swarmActive}
			<section class="space-y-1.5">
				{@render sectionHeading(m.swarm())}
				<div class="space-y-0.5">
					{@render infoRow(m.docker_info_swarm_state_label(), swarm.LocalNodeState, false)}
					{@render infoRow(m.docker_info_swarm_manager_label(), swarm.ControlAvailable ? m.common_yes() : m.common_no(), false)}
					{@render infoRow(m.docker_info_swarm_node_id_label(), swarm.NodeID, true)}
					{@render infoRow(m.docker_info_swarm_managers_label(), swarm.Managers ?? 0, false)}
					{@render infoRow(m.nodes(), swarm.Nodes ?? 0, false)}
				</div>
			</section>
		{/if}

		<section class="space-y-1.5">
			{@render sectionHeading(`${m.security()} & ${m.docker_info_runtimes()}`)}
			<div class="space-y-1">
				{@render tagGroup(m.docker_info_security_options(), info.SecurityOptions)}
				{@render tagGroup(m.docker_info_runtimes(), Object.keys(info.Runtimes ?? {}))}
			</div>
		</section>

		{#if info.DriverStatus && info.DriverStatus.length > 0}
			{@render storageDetailsSection(info.DriverStatus)}
		{/if}

		{#if info.Labels && info.Labels.length > 0}
			<section class="space-y-1.5">
				{@render sectionHeading(m.docker_info_labels_section())}
				<div class="flex flex-wrap gap-1">
					{#each withOccurrenceKeys(info.Labels) as { value: label, key } (key)}
						<Badge variant="outline" size="sm" mono>{label}</Badge>
					{/each}
				</div>
			</section>
		{/if}
	</div>
</div>
