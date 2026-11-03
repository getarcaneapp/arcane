<script lang="ts">
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Table from '#lib/components/ui/table/index.js';
	import { CpuIcon, HammerIcon, MemoryStickIcon, SecurityIcon, StatsIcon, VolumesIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PodInspect } from '#lib/types/podman.js';
	import { bytes } from '#lib/utils/formatting.js';

	interface Props {
		pod: PodInspect;
	}

	let { pod }: Props = $props();

	const cpu = $derived(
		[
			{ label: m.podman_pod_cpu_period(), value: pod.cpuPeriod ? String(pod.cpuPeriod) : undefined },
			{ label: m.podman_pod_cpu_quota(), value: pod.cpuQuota ? String(pod.cpuQuota) : undefined },
			{ label: m.cpu_shares(), value: pod.cpuShares ? String(pod.cpuShares) : undefined },
			{ label: m.podman_pod_cpuset_cpus(), value: pod.cpusetCpus },
			{ label: m.podman_pod_cpuset_mems(), value: pod.cpusetMems }
		].flatMap((entry) => (entry.value ? [{ label: entry.label, value: entry.value }] : []))
	);

	const memory = $derived(
		[
			{ label: m.podman_pod_memory(), value: pod.memoryLimit ? (bytes.format(pod.memoryLimit) ?? undefined) : undefined },
			{ label: m.podman_pod_memory_swap(), value: pod.memorySwap ? (bytes.format(pod.memorySwap) ?? undefined) : undefined }
		].flatMap((entry) => (entry.value ? [{ label: entry.label, value: entry.value }] : []))
	);

	const throttle = (rate: number) => `${bytes.format(rate) ?? rate}/s`;

	const blkio = $derived(
		[
			{ label: m.podman_pod_blkio_weight(), values: pod.blkioWeight ? [String(pod.blkioWeight)] : [] },
			{
				label: m.podman_pod_blkio_weight_devices(),
				values: (pod.blkioWeightDevices ?? []).map((device) => `${device.path} → ${device.weight}`)
			},
			{
				label: m.podman_pod_device_read_bps(),
				values: (pod.deviceReadBps ?? []).map((device) => `${device.path} → ${throttle(device.rate)}`)
			},
			{
				label: m.podman_pod_device_write_bps(),
				values: (pod.deviceWriteBps ?? []).map((device) => `${device.path} → ${throttle(device.rate)}`)
			}
		].filter((entry) => entry.values.length > 0)
	);

	const mounts = $derived(pod.mounts ?? []);
	const devices = $derived(pod.devices ?? []);
	const volumesFrom = $derived(pod.volumesFrom ?? []);
	const securityOpts = $derived(pod.securityOpts ?? []);
	const isEmpty = $derived(
		cpu.length === 0 &&
			memory.length === 0 &&
			blkio.length === 0 &&
			mounts.length === 0 &&
			devices.length === 0 &&
			volumesFrom.length === 0 &&
			securityOpts.length === 0
	);
</script>

{#if isEmpty}
	<EmptyState icon={StatsIcon} title={m.podman_pod_no_resources()} />
{:else}
	<div class="space-y-6">
		{#if cpu.length > 0}
			<DetailSection title={m.podman_pod_section_cpu()} icon={CpuIcon}>
				<KeyValueGrid>
					{#each cpu as entry (entry.label)}
						<KeyValueCard label={entry.label}>{entry.value}</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</DetailSection>
		{/if}

		{#if memory.length > 0}
			<DetailSection title={m.podman_pod_section_memory()} icon={MemoryStickIcon}>
				<KeyValueGrid>
					{#each memory as entry (entry.label)}
						<KeyValueCard label={entry.label}>{entry.value}</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</DetailSection>
		{/if}

		{#if blkio.length > 0}
			<DetailSection title={m.containers_block_io()} icon={StatsIcon}>
				<KeyValueGrid>
					{#each blkio as entry (entry.label)}
						<KeyValueCard label={entry.label} stacked>
							{#each entry.values as value (value)}
								<span>{value}</span>
							{/each}
						</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</DetailSection>
		{/if}

		{#if mounts.length > 0 || volumesFrom.length > 0}
			<DetailSection title={m.swarm_service_form_mounts()} icon={VolumesIcon}>
				{#if mounts.length > 0}
					<div class="overflow-x-auto rounded-lg border">
						<Table.Root aria-label={m.swarm_service_form_mounts()}>
							<Table.Header>
								<Table.Row>
									<Table.Head scope="col">{m.common_type()}</Table.Head>
									<Table.Head scope="col">{m.podman_pod_mount_source()}</Table.Head>
									<Table.Head scope="col">{m.destination()}</Table.Head>
									<Table.Head scope="col">{m.common_rw()}</Table.Head>
									<Table.Head scope="col">{m.podman_pod_volume_options()}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each mounts as mount (mount.destination)}
									<Table.Row>
										<Table.Cell>{mount.type}</Table.Cell>
										<Table.Cell><span class="font-mono text-xs break-all">{mount.name || mount.source || ''}</span></Table.Cell>
										<Table.Cell><span class="font-mono text-xs break-all">{mount.destination}</span></Table.Cell>
										<Table.Cell>{mount.rw ? m.common_yes() : m.common_no()}</Table.Cell>
										<Table.Cell><span class="font-mono text-xs">{mount.options?.join(', ') ?? ''}</span></Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				{/if}
				{#if volumesFrom.length > 0}
					<KeyValueGrid>
						<KeyValueCard label={m.podman_pod_volumes_from()}>{volumesFrom.join(', ')}</KeyValueCard>
					</KeyValueGrid>
				{/if}
			</DetailSection>
		{/if}

		{#if devices.length > 0}
			<DetailSection title={m.podman_pod_devices()} icon={HammerIcon}>
				<div class="overflow-x-auto rounded-lg border">
					<Table.Root aria-label={m.podman_pod_devices()}>
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col">{m.podman_pod_device_host_path()}</Table.Head>
								<Table.Head scope="col">{m.container_path()}</Table.Head>
								<Table.Head scope="col">{m.permissions()}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each devices as device (device.pathInContainer)}
								<Table.Row>
									<Table.Cell><span class="font-mono text-xs break-all">{device.pathOnHost}</span></Table.Cell>
									<Table.Cell><span class="font-mono text-xs break-all">{device.pathInContainer}</span></Table.Cell>
									<Table.Cell><span class="font-mono text-xs">{device.cgroupPermissions ?? ''}</span></Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			</DetailSection>
		{/if}

		{#if securityOpts.length > 0}
			<DetailSection title={m.common_security()} icon={SecurityIcon}>
				<KeyValueGrid>
					<KeyValueCard label={m.podman_pod_security_opt()} stacked>
						{#each securityOpts as option (option)}
							<span>{option}</span>
						{/each}
					</KeyValueCard>
				</KeyValueGrid>
			</DetailSection>
		{/if}
	</div>
{/if}
