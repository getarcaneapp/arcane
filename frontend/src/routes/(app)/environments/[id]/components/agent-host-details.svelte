<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { AgentHostInfo } from '#lib/types/environment.js';
	import { cn } from '#lib/utils.js';
	import { bytes } from '#lib/utils/formatting.js';

	let { host }: { host: AgentHostInfo } = $props();

	let stats = $derived(
		[
			{ label: m.hostname(), value: host.hostname, mono: false },
			{ label: m.docker_info_os_label(), value: host.os, mono: false },
			{ label: m.docker_info_kernel_version_label(), value: host.kernelVersion, mono: false },
			{ label: m.common_architecture(), value: host.architecture, mono: false },
			{ label: m.common_cpus(), value: host.cpus ? String(host.cpus) : undefined, mono: false },
			{ label: m.docker_info_memory_label(), value: host.memoryBytes ? bytes.format(host.memoryBytes) : undefined, mono: false },
			{ label: m.common_docker_version(), value: host.dockerVersion, mono: false },
			{ label: m.agent_version(), value: host.agentVersion, mono: false },
			{ label: m.public_ip(), value: host.publicIp, mono: true }
		].filter((stat) => Boolean(stat.value))
	);
</script>

{#if stats.length > 0}
	<section class="flex flex-col gap-4">
		<h2 class="px-1 text-base font-semibold">{m.host()}</h2>
		<div class="grid grid-cols-2 gap-x-6 gap-y-5 px-1 lg:grid-cols-4">
			{#each stats as stat (stat.label)}
				<div class="flex flex-col gap-0.5">
					<div class="text-2xs font-semibold tracking-wide text-muted-foreground uppercase">{stat.label}</div>
					<div class={cn('text-sm font-semibold break-all', stat.mono && 'font-mono select-all')}>{stat.value}</div>
				</div>
			{/each}
		</div>
	</section>
{/if}
