<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { CheckIcon, CloseIcon } from '#lib/icons/index.js';
	import type { DockerInfo } from '#lib/types/docker.js';
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';

	interface Props {
		open: boolean;
		dockerInfo: DockerInfo | null;
		dockerInfoPromise?: Promise<DockerInfo> | null;
		errorMessage?: string | null;
	}

	let { open = $bindable(), dockerInfo, dockerInfoPromise = null, errorMessage = null }: Props = $props();

	function handleClose() {
		open = false;
	}

	function formatTime(timeStr: string | undefined) {
		if (!timeStr) return '-';
		return formatDateTimeShort(timeStr) || timeStr;
	}

	function shortCommit(commit: { ID?: string } | undefined | null): string {
		const id = commit?.ID;
		if (!id) return '-';
		return id.length > 12 ? id.slice(0, 12) : id;
	}

	function swarmActive(info: DockerInfo): boolean {
		const state = (info.Swarm as { LocalNodeState?: string } | undefined)?.LocalNodeState;
		return !!state && state !== 'inactive';
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={(nextOpen) => (open = nextOpen)}
	title={m.docker_engine_title({ engine: dockerInfo?.Name ?? 'Docker Engine' })}
	description={m.docker_info_dialog_description()}
	contentClass="sm:max-w-[1100px]"
	showCloseButton={false}
>
	{#snippet children()}
		{#if dockerInfo}
			{@render dialogBody(dockerInfo)}
		{:else if dockerInfoPromise}
			{#await dockerInfoPromise then resolvedDockerInfo}
				{@render dialogBody(resolvedDockerInfo)}
			{:catch}
				<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
					<p class="text-sm font-medium">{errorMessage ?? m.common_failed()}</p>
				</div>
			{/await}
		{:else if errorMessage}
			<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
				<p class="text-sm font-medium">{errorMessage}</p>
			</div>
		{:else}
			<div class="flex min-h-56 flex-col items-center justify-center gap-3 pt-4">
				<Spinner class="size-5" />
				<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" onclick={handleClose} customLabel={m.common_close()} />
	{/snippet}
</ResponsiveDialog>

{#snippet dialogBody(info: DockerInfo)}
	<div class="space-y-3 pt-2">
		{#if info.Warnings && info.Warnings.length > 0}
			{@render warningsCard(info.Warnings)}
		{/if}

		{@render statStrip(info)}

		<div class="columns-1 gap-6 sm:columns-2 lg:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid">
			{@render systemSection(info)}
			{@render versionSection(info)}
			{@render configurationSection(info)}
			{@render networkSection(info)}
			{@render pluginsSection(info)}
			{@render capabilitiesSection(info)}
			{#if swarmActive(info)}
				{@render swarmSection(info)}
			{/if}
			{@render securitySection(info)}
			{#if info.DriverStatus && info.DriverStatus.length > 0}
				{@render storageDetailsSection(info)}
			{/if}
			{#if info.Labels && info.Labels.length > 0}
				{@render labelsSection(info)}
			{/if}
		</div>
	</div>
{/snippet}

{#snippet warningsCard(warnings: string[])}
	<div class="space-y-0.5 border-l-2 border-amber-500/50 pl-2.5">
		<h3 class="text-[10px] font-semibold tracking-wider text-amber-600 uppercase dark:text-amber-400">
			{m.docker_info_warnings_section()}
		</h3>
		<ul class="space-y-0.5">
			{#each warnings as warning, i (i)}
				<li class="text-xs [overflow-wrap:anywhere] text-amber-700 dark:text-amber-300">{warning}</li>
			{/each}
		</ul>
	</div>
{/snippet}

{#snippet statStrip(info: DockerInfo)}
	<div class="flex flex-wrap items-center gap-x-5 gap-y-1.5 border-b border-border/50 pb-2.5">
		{@render stat(m.common_running(), info.ContainersRunning ?? 0, 'bg-emerald-500')}
		{@render stat(m.paused(), info.ContainersPaused ?? 0, 'bg-amber-500')}
		{@render stat(m.common_stopped(), info.ContainersStopped ?? 0, 'bg-red-500')}
		{@render stat(m.images(), info.Images ?? 0, 'bg-blue-500')}
		<div class="hidden h-3 w-px bg-border/50 sm:block"></div>
		{@render stat(m.common_cpus(), info.NCPU ?? 0)}
		{@render stat(m.docker_info_memory_label(), (info.MemTotal ? bytes.format(info.MemTotal) : null) ?? '-')}
		{@render stat(m.goroutines(), info.NGoroutines ?? 0)}
		{@render stat(m.docker_info_file_descriptors(), info.NFd ?? 0)}
	</div>
{/snippet}

{#snippet stat(label: string, value: string | number, dot?: string)}
	<div class="flex items-baseline gap-1.5">
		{#if dot}
			<span class="size-1.5 shrink-0 self-center rounded-full {dot}"></span>
		{/if}
		<span class="text-sm font-semibold tabular-nums">{value}</span>
		<span class="text-[10px] tracking-tight whitespace-nowrap text-muted-foreground uppercase">{label}</span>
	</div>
{/snippet}

{#snippet systemSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_system_section()}
		</h3>
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
{/snippet}

{#snippet versionSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_version_section()}
		</h3>
		<div class="space-y-0.5">
			{@render infoRow(m.docker_info_server_version_label(), info.ServerVersion)}
			{@render infoRow(m.docker_info_api_version_label(), info.apiVersion)}
			{@render infoRow(m.go_version(), info.goVersion)}
			<div class="grid grid-cols-[minmax(104px,38%)_minmax(0,1fr)] items-baseline gap-x-3">
				<span class="text-[10px] leading-4 tracking-tight text-muted-foreground uppercase"
					>{m.docker_info_git_commit_label()}</span
				>
				<div class="flex items-center justify-end gap-1.5">
					<code class="rounded bg-muted px-1.5 py-0.5 text-xs">{info.gitCommit?.slice(0, 8) ?? '-'}</code>
					{#if info.gitCommit}
						<CopyButton text={info.gitCommit} size="icon" class="size-5" title={m.docker_copy_commit_hash()} />
					{/if}
				</div>
			</div>
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
{/snippet}

{#snippet configurationSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.common_configuration()}
		</h3>
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
{/snippet}

{#snippet capabilitiesSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_capabilities_section()}
		</h3>
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
{/snippet}

{#snippet storageDetailsSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_storage_details_section()}
		</h3>
		<div class="space-y-0.5">
			{#each info.DriverStatus ?? [] as entry, i (i)}
				{@render infoRow(entry[0] ?? '', entry[1], false)}
			{/each}
		</div>
	</section>
{/snippet}

{#snippet networkSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.resource_networks_cap()} & {m.docker_info_proxy_label()}
		</h3>
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
{/snippet}

{#snippet securitySection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.security()} & {m.docker_info_runtimes()}
		</h3>
		<div class="space-y-1">
			{@render tagGroup(m.docker_info_security_options(), info.SecurityOptions)}
			{@render tagGroup(m.docker_info_runtimes(), Object.keys(info.Runtimes ?? {}))}
		</div>
	</section>
{/snippet}

{#snippet pluginsSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_plugins_section()}
		</h3>
		<div class="space-y-1">
			{@render tagGroup(m.resource_volumes_cap(), info.Plugins?.Volume)}
			{@render tagGroup(m.resource_networks_cap(), info.Plugins?.Network)}
			{@render tagGroup(m.common_logs(), info.Plugins?.Log)}
			{@render tagGroup(m.docker_info_authorization_plugin(), info.Plugins?.Authorization)}
		</div>
	</section>
{/snippet}

{#snippet swarmSection(info: DockerInfo)}
	{@const swarm = info.Swarm as {
		LocalNodeState?: string;
		ControlAvailable?: boolean;
		NodeID?: string;
		Managers?: number;
		Nodes?: number;
	}}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.swarm()}
		</h3>
		<div class="space-y-0.5">
			{@render infoRow(m.docker_info_swarm_state_label(), swarm.LocalNodeState, false)}
			{@render infoRow(m.docker_info_swarm_manager_label(), swarm.ControlAvailable ? m.common_yes() : m.common_no(), false)}
			{@render infoRow(m.docker_info_swarm_node_id_label(), swarm.NodeID, true)}
			{@render infoRow(m.docker_info_swarm_managers_label(), swarm.Managers ?? 0, false)}
			{@render infoRow(m.nodes(), swarm.Nodes ?? 0, false)}
		</div>
	</section>
{/snippet}

{#snippet labelsSection(info: DockerInfo)}
	<section class="space-y-1.5">
		<h3 class="border-b border-border/50 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
			{m.docker_info_labels_section()}
		</h3>
		<div class="flex flex-wrap gap-1">
			{#each info.Labels ?? [] as label, i (i)}
				<Badge variant="outline" size="sm" class="font-mono">{label}</Badge>
			{/each}
		</div>
	</section>
{/snippet}

{#snippet tagGroup(label: string, items: string[] | undefined)}
	<div class="grid grid-cols-[minmax(96px,32%)_minmax(0,1fr)] items-baseline gap-x-3">
		<span class="text-[10px] leading-4 tracking-tight text-muted-foreground uppercase">{label}</span>
		<div class="flex flex-wrap justify-end gap-1">
			{#each items ?? [] as item, i (i)}
				<Badge variant="outline" size="sm">{item}</Badge>
			{:else}
				<span class="text-xs text-muted-foreground">-</span>
			{/each}
		</div>
	</div>
{/snippet}

{#snippet capRow(label: string, value: boolean | undefined)}
	<div class="flex items-center justify-between gap-2">
		<span class="text-[10px] tracking-tight [overflow-wrap:anywhere] text-muted-foreground uppercase">{label}</span>
		{#if value}
			<CheckIcon class="size-3 shrink-0 text-emerald-600 dark:text-emerald-400" />
			<span class="sr-only">{m.common_yes()}</span>
		{:else}
			<CloseIcon class="size-3 shrink-0 text-muted-foreground/50" />
			<span class="sr-only">{m.common_no()}</span>
		{/if}
	</div>
{/snippet}

{#snippet blockRow(label: string, value: string)}
	<div class="space-y-0.5 pt-0.5">
		<div class="text-[10px] tracking-tight text-muted-foreground uppercase">{label}</div>
		<div
			class="max-h-48 overflow-y-auto rounded-md bg-muted/30 px-2 py-1 font-mono text-[11px] leading-relaxed break-all text-foreground/80"
		>
			{value}
		</div>
	</div>
{/snippet}

{#snippet infoRow(label: string, value: string | number | undefined | null, mono: boolean = true)}
	<div class="grid grid-cols-[minmax(104px,38%)_minmax(0,1fr)] items-baseline gap-x-3">
		<span class="text-[10px] leading-4 tracking-tight text-muted-foreground uppercase">{label}</span>
		<span class="text-right text-xs leading-4 [overflow-wrap:anywhere] {mono ? 'font-mono' : ''}">
			{value === undefined || value === null || value === '' ? '-' : value}
		</span>
	</div>
{/snippet}
