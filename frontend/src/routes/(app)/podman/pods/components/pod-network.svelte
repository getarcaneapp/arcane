<script lang="ts">
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { GlobeIcon, NetworksIcon, SettingsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PodInfraConfig } from '#lib/types/podman.js';

	interface Props {
		infraConfig: PodInfraConfig | undefined;
	}

	let { infraConfig }: Props = $props();

	const yesNo = (value: boolean) => (value ? m.common_yes() : m.common_no());

	const networking = $derived(
		[
			{ label: m.podman_pod_host_network(), value: infraConfig ? yesNo(infraConfig.hostNetwork) : undefined },
			{ label: m.resource_networks_cap(), value: infraConfig?.networks?.join(', ') },
			{ label: m.containers_ip_address(), value: infraConfig?.staticIp },
			{ label: m.containers_mac_address(), value: infraConfig?.staticMac }
		].flatMap((entry) => (entry.value ? [{ label: entry.label, value: entry.value }] : []))
	);

	const portBindings = $derived(
		Object.entries(infraConfig?.portBindings ?? {}).map(([containerPort, bindings]) => ({
			containerPort,
			hosts: (bindings ?? []).map(
				(binding) => `${binding.hostIp ? `${binding.hostIp}:` : ''}${binding.hostPort} → ${containerPort}`
			)
		}))
	);

	const dnsHosts = $derived(
		[
			{ label: m.podman_pod_dns_servers(), value: infraConfig?.dnsServer?.join(', ') },
			{ label: m.podman_pod_dns_search(), value: infraConfig?.dnsSearch?.join(', ') },
			{ label: m.podman_pod_dns_options(), value: infraConfig?.dnsOption?.join(', ') },
			{ label: m.podman_pod_add_hosts(), value: infraConfig?.hostAdd?.join(', ') },
			{ label: m.podman_pod_hosts_file(), value: infraConfig?.hostsFile },
			{ label: m.podman_pod_no_resolv_conf(), value: infraConfig ? yesNo(infraConfig.noManageResolvConf) : undefined },
			{ label: m.podman_pod_no_hosts(), value: infraConfig ? yesNo(infraConfig.noManageHosts) : undefined },
			{ label: m.podman_pod_no_hostname(), value: infraConfig ? yesNo(infraConfig.noManageHostname) : undefined }
		].flatMap((entry) => (entry.value ? [{ label: entry.label, value: entry.value }] : []))
	);

	const networkOptions = $derived(Object.entries(infraConfig?.networkOptions ?? {}));
</script>

{#if !infraConfig}
	<EmptyState icon={NetworksIcon} title={m.podman_pod_no_network_config()} />
{:else}
	<div class="space-y-6">
		<DetailSection title={m.podman_pod_section_networking()} icon={NetworksIcon}>
			<KeyValueGrid>
				{#each networking as entry (entry.label)}
					<KeyValueCard label={entry.label} valueTitle={m.common_click_to_select()}>{entry.value}</KeyValueCard>
				{/each}
			</KeyValueGrid>
		</DetailSection>

		{#if portBindings.length > 0}
			<DetailSection title={m.common_port_mappings()} icon={GlobeIcon}>
				<KeyValueGrid>
					{#each portBindings as binding (binding.containerPort)}
						<KeyValueCard label={binding.containerPort}>{binding.hosts.join(', ')}</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</DetailSection>
		{/if}

		<DetailSection title={m.podman_pod_section_dns_hosts()} icon={GlobeIcon}>
			<KeyValueGrid>
				{#each dnsHosts as entry (entry.label)}
					<KeyValueCard label={entry.label} valueTitle={m.common_click_to_select()}>{entry.value}</KeyValueCard>
				{/each}
			</KeyValueGrid>
		</DetailSection>

		{#if networkOptions.length > 0}
			<DetailSection title={m.podman_pod_network_options()} icon={SettingsIcon}>
				<KeyValueGrid>
					{#each networkOptions as [key, values] (key)}
						<KeyValueCard label={key}>{values.join(', ')}</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</DetailSection>
		{/if}
	</div>
{/if}
