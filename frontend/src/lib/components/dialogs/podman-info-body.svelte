<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { PodmanInfo, PodmanInfoIdMap, PodmanStatus } from '#lib/types/podman.js';
	import { bytes } from '#lib/utils/formatting.js';

	import {
		capRow,
		commitRow,
		formatTime,
		presentRow,
		sectionHeading,
		stat,
		storageDetailsSection,
		tagGroup
	} from './engine-info-parts.svelte';

	let { info, status, podCount }: { info: PodmanInfo; status: PodmanStatus; podCount: number } = $props();

	function firstLine(value: string | undefined): string {
		return value?.split('\n')[0]?.trim() ?? '';
	}

	function formatIdMap(entries: PodmanInfoIdMap[] | undefined): string {
		return (entries ?? []).map((entry) => `${entry.container_id}:${entry.host_id}:${entry.size}`).join(', ');
	}

	const host = $derived(info.host);
	const store = $derived(info.store);
	const distribution = $derived.by(() => {
		const { distribution: name, variant, version, codename } = host.distribution;
		const label = [name, variant, version === 'unknown' ? '' : version].filter(Boolean).join(' ');
		return codename ? `${label} (${codename})` : label;
	});
	const rootlessNetworkHelper = $derived(host.rootlessNetworkCmd === 'slirp4netns' ? host.slirp4netns : host.pasta);
	const capabilities = $derived(
		(host.security.capabilities ?? '')
			.split(',')
			.map((capability) => capability.trim())
			.filter(Boolean)
	);
	const storageDetails = $derived(Object.entries(store.graphStatus ?? {}));
</script>

<div class="space-y-3 pt-2">
	<div class="flex flex-wrap items-center gap-x-5 gap-y-1.5 border-b border-border/50 pb-2.5">
		{@render stat(m.common_running(), store.containerStore.running, 'bg-success')}
		{@render stat(m.paused(), store.containerStore.paused, 'bg-warning')}
		{@render stat(m.common_stopped(), store.containerStore.stopped, 'bg-destructive')}
		{@render stat(m.podman_pods_title(), podCount, 'bg-primary')}
		{@render stat(m.images(), store.imageStore.number, 'bg-info')}
		<div class="hidden h-3 w-px bg-border/50 sm:block"></div>
		{@render stat(m.common_cpus(), host.cpus)}
		{@render stat(m.docker_info_memory_label(), (host.memTotal ? bytes.format(host.memTotal) : null) ?? '-')}
	</div>

	<div class="columns-1 gap-6 sm:columns-2 lg:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid">
		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_system_section())}
			<div class="space-y-0.5">
				{@render presentRow(m.hostname(), host.hostname)}
				{@render presentRow(m.docker_info_os_label(), distribution, false)}
				{@render presentRow(m.docker_info_kernel_version_label(), host.kernel)}
				{@render presentRow(m.common_architecture(), host.arch)}
				{@render presentRow(m.podman_info_swap(), host.swapTotal ? bytes.format(host.swapTotal) : null, false)}
				{@render presentRow(m.common_uptime(), host.uptime, false)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.podman())}
			<div class="space-y-0.5">
				{@render presentRow(m.common_version(), info.version.version)}
				{@render presentRow(m.docker_info_api_version_label(), info.version.apiVersion)}
				{@render presentRow(m.go_version(), info.version.goVersion)}
				{@render presentRow(m.build_time(), info.version.builtTime ? formatTime(info.version.builtTime) : '', false)}
				{#if info.version.gitCommit}
					{@render commitRow(info.version.gitCommit)}
				{/if}
				{@render presentRow(m.common_mode(), host.security.rootless ? m.podman_info_rootless() : m.podman_info_rootful(), false)}
				{@render presentRow(m.podman_info_database_backend(), host.databaseBackend)}
				{@render presentRow(m.podman_info_event_logger(), host.eventLogger)}
				{@render presentRow(m.docker_info_logging_driver_label(), host.logDriver)}
				{@render presentRow(m.podman_info_remote_service(), host.serviceIsRemote ? m.common_yes() : m.common_no(), false)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.runtime())}
			<div class="space-y-1">
				{@render presentRow(m.podman_info_oci_runtime(), firstLine(host.ociRuntime.version) || host.ociRuntime.name)}
				{@render presentRow(m.podman_info_conmon(), firstLine(host.conmon.version))}
				{@render presentRow(m.docker_info_cgroup_driver_label(), host.cgroupManager)}
				{@render presentRow(m.docker_info_cgroup_version_label(), host.cgroupVersion)}
				{#if host.cgroupControllers && host.cgroupControllers.length > 0}
					{@render tagGroup(m.podman_info_cgroup_controllers(), host.cgroupControllers)}
				{/if}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.podman_pod_section_networking())}
			<div class="space-y-1">
				{@render presentRow(m.podman_info_network_backend(), host.networkBackendInfo.version || host.networkBackend)}
				{@render presentRow(m.podman_info_dns(), host.networkBackendInfo.dns.version)}
				{@render presentRow(m.podman_info_default_network(), host.networkBackendInfo.defaultNetwork)}
				{#if host.security.rootless}
					{@render presentRow(
						m.podman_info_rootless_network(),
						firstLine(rootlessNetworkHelper.version) || host.rootlessNetworkCmd
					)}
				{/if}
				{#if info.registries.search && info.registries.search.length > 0}
					{@render tagGroup(m.podman_info_search_registries(), info.registries.search)}
				{/if}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.security())}
			<div class="grid grid-cols-2 gap-x-4 gap-y-0.5">
				{@render capRow(m.podman_info_rootless(), host.security.rootless)}
				{@render capRow(m.podman_info_selinux(), host.security.selinuxEnabled)}
				{@render capRow(m.podman_info_apparmor(), host.security.apparmorEnabled)}
				{@render capRow(m.podman_info_seccomp(), host.security.seccompEnabled)}
			</div>
			<div class="space-y-1">
				{@render presentRow(m.podman_info_seccomp_profile(), host.security.seccompProfilePath)}
				{@render presentRow(m.podman_info_uid_map(), formatIdMap(host.idMappings.uidmap))}
				{@render presentRow(m.podman_info_gid_map(), formatIdMap(host.idMappings.gidmap))}
				{#if capabilities.length > 0}
					{@render tagGroup(m.podman_info_capabilities(), capabilities)}
				{/if}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.storage())}
			<div class="space-y-0.5">
				{@render presentRow(m.docker_info_storage_driver_label(), store.graphDriverName)}
				{@render presentRow(m.docker_info_root_dir(), store.graphRoot)}
				{@render presentRow(m.podman_info_run_root(), store.runRoot)}
				{@render presentRow(m.podman_info_volume_path(), store.volumePath)}
			</div>
		</section>

		{#if storageDetails.length > 0}
			{@render storageDetailsSection(storageDetails)}
		{/if}

		<section class="space-y-1.5">
			{@render sectionHeading(m.features_title())}
			<div class="grid grid-cols-2 gap-x-4 gap-y-0.5">
				{@render capRow(m.podman_quadlets_title(), status.quadletsSupported)}
				{@render capRow(m.podman_info_quadlet_apps(), status.quadletAppsSupported)}
				{@render capRow(m.podman_info_systemd(), status.systemdAvailable)}
			</div>
			<div class="space-y-0.5">
				{@render presentRow(m.common_scope(), status.systemdScope, false)}
				{@render presentRow(
					m.auto_update_title(),
					status.autoUpdateMode === 'unavailable' ? m.common_unavailable() : status.autoUpdateMode,
					false
				)}
			</div>
		</section>

		<section class="space-y-1.5">
			{@render sectionHeading(m.docker_info_plugins_section())}
			<div class="space-y-1">
				{#if info.plugins.volume && info.plugins.volume.length > 0}
					{@render tagGroup(m.resource_volumes_cap(), info.plugins.volume)}
				{/if}
				{#if info.plugins.network && info.plugins.network.length > 0}
					{@render tagGroup(m.resource_networks_cap(), info.plugins.network)}
				{/if}
				{#if info.plugins.log && info.plugins.log.length > 0}
					{@render tagGroup(m.common_logs(), info.plugins.log)}
				{/if}
			</div>
		</section>
	</div>
</div>
