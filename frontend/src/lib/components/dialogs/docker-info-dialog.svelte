<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { Spinner } from '$lib/components/ui/spinner';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import { m } from '$lib/paraglide/messages';
	import bytes from '$lib/utils/bytes';

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
		try {
			return new Date(timeStr).toLocaleString();
		} catch {
			return timeStr;
		}
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
				<p class="text-muted-foreground text-sm">{m.common_loading()}</p>
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" onclick={handleClose} customLabel={m.common_close()} />
	{/snippet}
</ResponsiveDialog>

{#snippet dialogBody(info: DockerInfo)}
	<div class="space-y-6 pt-4">
		<div class="grid gap-6">
			{@render statsSection(info)}
			{@render resourcesSection(info)}
		</div>

		<div class="grid gap-6 lg:grid-cols-2 xl:grid-cols-3">
			{@render systemSection(info)}
			{@render versionSection(info)}
			{@render configurationSection(info)}
		</div>

		<div class="grid gap-6 lg:grid-cols-2 xl:grid-cols-3">
			{@render networkSection(info)}
			{@render securitySection(info)}
			{@render pluginsSection(info)}
		</div>
	</div>
{/snippet}

{#snippet statsSection(info: DockerInfo)}
	<div>
		<h3 class="text-muted-foreground mb-2 text-xs font-semibold tracking-wider uppercase">
			{m.docker_info_stats_section()}
		</h3>
		<div class="grid gap-3 sm:grid-cols-4">
			{@render statCard(m.common_running(), info.ContainersRunning ?? 0, 'emerald')}
			{@render statCard(m.docker_info_paused_label(), info.ContainersPaused ?? 0, 'amber')}
			{@render statCard(m.common_stopped(), info.ContainersStopped ?? 0, 'red')}
			{@render statCard(m.docker_info_images_label(), info.Images ?? 0, 'blue')}
		</div>
	</div>
{/snippet}

{#snippet systemSection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.docker_info_system_section()}
		</h3>
		<div class="space-y-1.5 rounded-lg border p-3">
			{@render infoRow(m.common_name(), info.Name)}
			{@render infoRow(m.common_id(), info.ID, true)}
			{@render infoRow(m.docker_info_os_label(), info.OperatingSystem)}
			{@render infoRow(m.docker_info_os_type_label(), info.OSType)}
			{@render infoRow(m.common_architecture(), info.Architecture)}
			{@render infoRow(m.docker_info_kernel_version_label(), info.KernelVersion)}
			{@render infoRow(m.docker_info_system_time(), formatTime(info.SystemTime), false)}
			{@render infoRow(m.docker_info_root_dir(), info.DockerRootDir, true)}
		</div>
	</div>
{/snippet}

{#snippet versionSection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.docker_info_version_section()}
		</h3>
		<div class="space-y-1.5 rounded-lg border p-3">
			{@render infoRow(m.docker_info_server_version_label(), info.ServerVersion)}
			{@render infoRow(m.docker_info_api_version_label(), info.apiVersion)}
			{@render infoRow(m.docker_info_go_version_label(), info.goVersion)}
			<div class="flex items-center justify-between gap-4">
				<span class="text-muted-foreground text-xs">{m.docker_info_git_commit_label()}</span>
				<div class="flex items-center gap-2">
					<code class="bg-muted rounded px-1.5 py-0.5 text-xs">{info.gitCommit?.slice(0, 8) ?? '-'}</code>
					{#if info.gitCommit}
						<CopyButton text={info.gitCommit} size="icon" class="size-6" title="Copy full commit hash" />
					{/if}
				</div>
			</div>
			{@render infoRow(m.docker_info_build_time_label(), formatTime(info.buildTime), false)}
			{@render infoRow(m.docker_info_experimental(), info.ExperimentalBuild ? m.common_yes() : m.common_no(), false)}
		</div>
	</div>
{/snippet}

{#snippet resourcesSection(info: DockerInfo)}
	<div>
		<h3 class="text-muted-foreground mb-2 text-xs font-semibold tracking-wider uppercase">
			{m.docker_info_resources_section()}
		</h3>
		<div class="grid gap-3 sm:grid-cols-4">
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.common_cpus()}</div>
				<div class="flex items-center gap-2">
					<Badge variant="outline" class="text-sm font-bold">{info.NCPU ?? 0}</Badge>
					<span class="text-muted-foreground text-[10px]">cores</span>
				</div>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_memory_label()}</div>
				<Badge variant="outline" class="text-sm font-bold">{info.MemTotal ? bytes.format(info.MemTotal) : '-'}</Badge>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_goroutines()}</div>
				<Badge variant="outline" class="text-sm font-bold">{info.NGoroutines ?? 0}</Badge>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_file_descriptors()}</div>
				<Badge variant="outline" class="text-sm font-bold">{info.NFd ?? 0}</Badge>
			</div>
		</div>
	</div>
{/snippet}

{#snippet configurationSection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.common_configuration()}
		</h3>
		<div class="space-y-1.5 rounded-lg border p-3">
			{@render infoRow(m.docker_info_storage_driver_label(), info.Driver)}
			{@render infoRow(m.docker_info_logging_driver_label(), info.LoggingDriver)}
			{@render infoRow(m.docker_info_cgroup_driver_label(), info.CgroupDriver)}
			{@render infoRow(m.docker_info_cgroup_version_label(), info.CgroupVersion)}
			{@render infoRow(m.docker_info_isolation(), info.Isolation)}
			{@render infoRow(m.docker_info_init_binary(), info.InitBinary)}
			{@render infoRow(m.docker_info_default_runtime(), info.DefaultRuntime)}
		</div>
	</div>
{/snippet}

{#snippet networkSection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.resource_networks_cap()} & {m.docker_info_proxy_label()}
		</h3>
		<div class="space-y-1.5 rounded-lg border p-3">
			{@render infoRow(m.docker_info_ipv4_forwarding(), info.IPv4Forwarding ? m.common_enabled() : m.common_disabled(), false)}
			{@render infoRow(m.docker_info_http_proxy(), info.HttpProxy)}
			{@render infoRow(m.docker_info_https_proxy(), info.HttpsProxy)}
			{@render infoRow(m.docker_info_no_proxy(), info.NoProxy)}
			{@render infoRow(m.docker_info_bridge_ip(), info.DefaultAddressPools?.[0]?.Base)}
		</div>
	</div>
{/snippet}

{#snippet pluginsSection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.docker_info_plugins_section()}
		</h3>
		<div class="space-y-3 rounded-lg border p-3">
			<div>
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.resource_volumes_cap()}</div>
				<div class="flex flex-wrap gap-1">
					{#each info.Plugins?.Volume ?? [] as plugin}
						<Badge variant="outline" class="px-1.5 py-0 text-[10px]">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div>
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.resource_networks_cap()}</div>
				<div class="flex flex-wrap gap-1">
					{#each info.Plugins?.Network ?? [] as plugin}
						<Badge variant="outline" class="px-1.5 py-0 text-[10px]">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div>
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_logs_plugin()}</div>
				<div class="flex flex-wrap gap-1">
					{#each info.Plugins?.Log ?? [] as plugin}
						<Badge variant="outline" class="px-1.5 py-0 text-[10px]">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet securitySection(info: DockerInfo)}
	<div class="space-y-2">
		<h3 class="text-muted-foreground text-xs font-semibold tracking-wider uppercase">
			{m.security_title()} & {m.docker_info_runtimes()}
		</h3>
		<div class="space-y-3 rounded-lg border p-3">
			<div>
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_security_options()}</div>
				<div class="flex flex-wrap gap-1">
					{#each info.SecurityOptions ?? [] as opt}
						<Badge variant="outline" class="px-1.5 py-0 text-[10px]">{opt}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div>
				<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{m.docker_info_runtimes()}</div>
				<div class="flex flex-wrap gap-1">
					{#each Object.keys(info.Runtimes ?? {}) as runtime}
						<Badge variant="outline" class="px-1.5 py-0 text-[10px]">{runtime}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet statCard(label: string, value: number, color: 'emerald' | 'amber' | 'red' | 'blue' | 'neutral')}
	{@const colors = {
		emerald: {
			bg: 'bg-emerald-500/5',
			badge: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-600 dark:text-emerald-300'
		},
		amber: {
			bg: 'bg-amber-500/5',
			badge: 'border-amber-500/30 bg-amber-500/15 text-amber-700 dark:text-amber-300'
		},
		red: {
			bg: 'bg-red-500/5',
			badge: 'border-red-500/30 bg-red-500/15 text-red-600 dark:text-red-300'
		},
		blue: {
			bg: 'bg-blue-500/5',
			badge: 'border-blue-500/30 bg-blue-500/15 text-blue-600 dark:text-blue-300'
		},
		neutral: {
			bg: '',
			badge: ''
		}
	}}
	<div class="rounded-lg border p-3 {colors[color].bg}">
		<div class="text-muted-foreground mb-1 text-[10px] tracking-tight uppercase">{label}</div>
		<Badge variant="outline" class="{colors[color].badge} rounded-md text-base font-bold tabular-nums">
			{value}
		</Badge>
	</div>
{/snippet}

{#snippet infoRow(label: string, value: string | undefined | null, mono: boolean = true)}
	<div class="grid grid-cols-[minmax(112px,38%)_minmax(0,1fr)] items-start gap-x-4 gap-y-1">
		<span class="text-muted-foreground text-[10px] tracking-tight uppercase">{label}</span>
		<span class="text-right text-xs [overflow-wrap:anywhere] {mono ? 'font-mono' : ''}">
			{value || '-'}
		</span>
	</div>
{/snippet}
