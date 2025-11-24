<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { Separator } from '$lib/components/ui/separator';
	import ServerIcon from '@lucide/svelte/icons/server';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import ShieldIcon from '@lucide/svelte/icons/shield';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import { m } from '$lib/paraglide/messages';
	import bytes from 'bytes';

	interface Props {
		open: boolean;
		dockerInfo: DockerInfo | null;
	}

	let { open = $bindable(), dockerInfo }: Props = $props();

	const shortGitCommit = $derived(dockerInfo?.gitCommit?.slice(0, 8) ?? '-');
	const formattedMemory = $derived(dockerInfo?.MemTotal ? bytes.format(dockerInfo.MemTotal) : '-');

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
	bind:open
	title={m.docker_engine_title({ engine: dockerInfo?.Name ?? 'Docker Engine' })}
	description={m.docker_info_dialog_description()}
	contentClass="sm:max-w-[800px]"
>
	{#snippet children()}
		<div class="space-y-6 pt-4">
			{@render statsSection()}

			<div class="grid gap-6 sm:grid-cols-2">
				{@render systemSection()}
				{@render versionSection()}
			</div>

			{@render resourcesSection()}

			<div class="grid gap-6 sm:grid-cols-2">
				{@render configurationSection()}
				{@render networkSection()}
			</div>

			{@render pluginsSection()}
			{@render securitySection()}
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={handleClose}>
			{m.common_close()}
		</Button>
	{/snippet}
</ResponsiveDialog>

{#snippet statsSection()}
	<div>
		<h3 class="mb-3 flex items-center gap-2 text-sm font-semibold">
			<ActivityIcon class="size-4" />
			{m.docker_info_stats_section()}
		</h3>
		<div class="space-y-3">
			<div class="grid gap-3 sm:grid-cols-4">
				{@render statCard(m.common_running(), dockerInfo?.ContainersRunning ?? 0, 'emerald')}
				{@render statCard(m.docker_info_paused_label(), dockerInfo?.ContainersPaused ?? 0, 'amber')}
				{@render statCard(m.common_stopped(), dockerInfo?.ContainersStopped ?? 0, 'red')}
				{@render statCard(m.docker_info_images_label(), dockerInfo?.Images ?? 0, 'neutral', true)}
			</div>
		</div>
	</div>
{/snippet}

{#snippet systemSection()}
	<div class="space-y-3">
		<h3 class="flex items-center gap-2 text-sm font-semibold">
			<ServerIcon class="size-4" />
			{m.docker_info_system_section()}
		</h3>
		<div class="space-y-2 rounded-lg border p-3">
			{@render infoRow(m.common_name(), dockerInfo?.Name)}
			{@render infoRow(m.common_id(), dockerInfo?.ID, true, true)}
			{@render infoRow(m.docker_info_os_label(), dockerInfo?.OperatingSystem)}
			{@render infoRow(m.docker_info_os_type_label(), dockerInfo?.OSType)}
			{@render infoRow(m.common_architecture(), dockerInfo?.Architecture)}
			{@render infoRow(m.docker_info_kernel_version_label(), dockerInfo?.KernelVersion)}
			{@render infoRow(m.docker_info_system_time(), formatTime(dockerInfo?.SystemTime), false)}
			{@render infoRow(m.docker_info_root_dir(), dockerInfo?.DockerRootDir, true, true)}
		</div>
	</div>
{/snippet}

{#snippet versionSection()}
	<div class="space-y-3">
		<h3 class="flex items-center gap-2 text-sm font-semibold">
			<LayersIcon class="size-4" />
			{m.docker_info_version_section()}
		</h3>
		<div class="space-y-2 rounded-lg border p-3">
			{@render infoRow(m.docker_info_server_version_label(), dockerInfo?.ServerVersion)}
			{@render infoRow(m.docker_info_api_version_label(), dockerInfo?.apiVersion)}
			{@render infoRow(m.docker_info_go_version_label(), dockerInfo?.goVersion)}
			<div class="flex items-center justify-between gap-4">
				<span class="text-muted-foreground text-xs">{m.docker_info_git_commit_label()}</span>
				<div class="flex items-center gap-2">
					<code class="bg-muted rounded px-1.5 py-0.5 text-xs">{shortGitCommit}</code>
					{#if dockerInfo?.gitCommit}
						<CopyButton text={dockerInfo.gitCommit} size="icon" class="size-7" title="Copy full commit hash" />
					{/if}
				</div>
			</div>
			{@render infoRow(m.docker_info_build_time_label(), formatTime(dockerInfo?.buildTime), false)}
			{@render infoRow(m.docker_info_experimental(), dockerInfo?.ExperimentalBuild ? m.common_yes() : m.common_no(), false)}
		</div>
	</div>
{/snippet}

{#snippet resourcesSection()}
	<div>
		<h3 class="mb-3 flex items-center gap-2 text-sm font-semibold">
			<CpuIcon class="size-4" />
			{m.docker_info_resources_section()}
		</h3>
		<div class="grid gap-3 sm:grid-cols-4">
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.common_cpus()}</div>
				<div class="flex items-center gap-2">
					<Badge variant="secondary" class="text-base">{dockerInfo?.NCPU ?? 0}</Badge>
					<span class="text-muted-foreground text-xs">cores</span>
				</div>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_memory_label()}</div>
				<Badge variant="secondary" class="text-base">{formattedMemory}</Badge>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_goroutines()}</div>
				<Badge variant="secondary" class="text-base">{dockerInfo?.NGoroutines ?? 0}</Badge>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_file_descriptors()}</div>
				<Badge variant="secondary" class="text-base">{dockerInfo?.NFd ?? 0}</Badge>
			</div>
		</div>
	</div>
{/snippet}

{#snippet configurationSection()}
	<div class="space-y-3">
		<h3 class="flex items-center gap-2 text-sm font-semibold">
			<HardDriveIcon class="size-4" />
			{m.common_configuration()}
		</h3>
		<div class="space-y-2 rounded-lg border p-3">
			{@render infoRow(m.docker_info_storage_driver_label(), dockerInfo?.Driver)}
			{@render infoRow(m.docker_info_logging_driver_label(), dockerInfo?.LoggingDriver)}
			{@render infoRow(m.docker_info_cgroup_driver_label(), dockerInfo?.CgroupDriver)}
			{@render infoRow(m.docker_info_cgroup_version_label(), dockerInfo?.CgroupVersion)}
			{@render infoRow(m.docker_info_isolation(), dockerInfo?.Isolation)}
			{@render infoRow(m.docker_info_init_binary(), dockerInfo?.InitBinary)}
			{@render infoRow(m.docker_info_default_runtime(), dockerInfo?.DefaultRuntime)}
		</div>
	</div>
{/snippet}

{#snippet networkSection()}
	<div class="space-y-3">
		<h3 class="flex items-center gap-2 text-sm font-semibold">
			<NetworkIcon class="size-4" />
			{m.resource_networks_cap()} & {m.docker_info_proxy_label()}
		</h3>
		<div class="space-y-2 rounded-lg border p-3">
			{@render infoRow(
				m.docker_info_ipv4_forwarding(),
				dockerInfo?.IPv4Forwarding ? m.common_enabled() : m.common_disabled(),
				false
			)}
			{@render infoRow(m.docker_info_http_proxy(), dockerInfo?.HttpProxy)}
			{@render infoRow(m.docker_info_https_proxy(), dockerInfo?.HttpsProxy)}
			{@render infoRow(m.docker_info_no_proxy(), dockerInfo?.NoProxy)}
			{@render infoRow(m.docker_info_bridge_ip(), dockerInfo?.DefaultAddressPools?.[0]?.Base)}
		</div>
	</div>
{/snippet}

{#snippet pluginsSection()}
	<div>
		<h3 class="mb-3 flex items-center gap-2 text-sm font-semibold">
			<LayersIcon class="size-4" />
			{m.docker_info_plugins_section()}
		</h3>
		<div class="grid gap-3 sm:grid-cols-3">
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.resource_volumes_cap()}</div>
				<div class="flex flex-wrap gap-1">
					{#each dockerInfo?.Plugins?.Volume ?? [] as plugin}
						<Badge variant="outline" class="text-xs">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.resource_networks_cap()}</div>
				<div class="flex flex-wrap gap-1">
					{#each dockerInfo?.Plugins?.Network ?? [] as plugin}
						<Badge variant="outline" class="text-xs">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_logs_plugin()}</div>
				<div class="flex flex-wrap gap-1">
					{#each dockerInfo?.Plugins?.Log ?? [] as plugin}
						<Badge variant="outline" class="text-xs">{plugin}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet securitySection()}
	<div>
		<h3 class="mb-3 flex items-center gap-2 text-sm font-semibold">
			<ShieldIcon class="size-4" />
			{m.security_title()} & {m.docker_info_runtimes()}
		</h3>
		<div class="grid gap-3 sm:grid-cols-2">
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_security_options()}</div>
				<div class="flex flex-wrap gap-1">
					{#each dockerInfo?.SecurityOptions ?? [] as opt}
						<Badge variant="secondary" class="text-xs">{opt}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
			<div class="rounded-lg border p-3">
				<div class="text-muted-foreground mb-2 text-xs">{m.docker_info_runtimes()}</div>
				<div class="flex flex-wrap gap-1">
					{#each Object.keys(dockerInfo?.Runtimes ?? {}) as runtime}
						<Badge variant="outline" class="text-xs">{runtime}</Badge>
					{:else}
						<span class="text-muted-foreground text-xs">-</span>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet statCard(label: string, value: number, color: 'emerald' | 'amber' | 'red' | 'neutral', outline: boolean = false)}
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
		neutral: {
			bg: '',
			badge: ''
		}
	}}
	<div class="rounded-lg border p-3 {colors[color].bg}">
		<div class="text-muted-foreground mb-2 text-xs">{label}</div>
		{#if color === 'neutral'}
			<div class="text-2xl font-semibold tabular-nums">{value}</div>
		{:else}
			<Badge variant={outline ? 'outline' : undefined} class="{colors[color].badge} text-lg">
				{value}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet infoRow(label: string, value: string | undefined | null, mono: boolean = true, truncate: boolean = false)}
	<div class="flex items-center justify-between gap-4">
		<span class="text-muted-foreground shrink-0 text-xs">{label}</span>
		<span class="text-sm {mono ? 'font-mono' : ''} {truncate ? 'max-w-[200px] truncate' : ''}" title={value ?? ''}>
			{value || '-'}
		</span>
	</div>
{/snippet}
