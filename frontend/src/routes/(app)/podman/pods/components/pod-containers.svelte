<script lang="ts">
	import { PortBadge } from '#lib/components/badges/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ContainersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PodmanPodInspect } from '#lib/types/podman.js';
	import { environmentHref } from '#lib/utils/environments.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	interface Props {
		pod: PodmanPodInspect;
		envId: string;
	}

	let { pod, envId }: Props = $props();

	const members = $derived(pod.containers ?? []);
</script>

<Card.Root>
	<Card.Header icon={ContainersIcon}>
		<Card.Title>
			<h2>{m.containers()}</h2>
		</Card.Title>
	</Card.Header>
	{#if members.length === 0}
		<EmptyState icon={ContainersIcon} title={m.podman_pod_no_containers()} />
	{:else}
		<div class="overflow-x-auto rounded-b-lg">
			<Table.Root aria-label={m.containers()}>
				<Table.Header>
					<Table.Row>
						<Table.Head scope="col">{m.common_name()}</Table.Head>
						<Table.Head scope="col">{m.common_image()}</Table.Head>
						<Table.Head scope="col">{m.common_state()}</Table.Head>
						<Table.Head scope="col">{m.common_status()}</Table.Head>
						<Table.Head scope="col">{m.common_ports()}</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each members as member (member.id)}
						<Table.Row>
							<Table.Cell>
								<div class="flex items-center gap-2">
									<a
										class="min-w-0 truncate font-medium hover:underline"
										href={environmentHref(`/containers/${member.id}`, envId)}>{member.name}</a
									>
									{#if member.isInfra || member.id === pod.infraContainerId}
										<Badge variant="gray" size="sm">{m.podman_pod_infra()}</Badge>
									{/if}
								</div>
							</Table.Cell>
							<Table.Cell><span class="font-mono text-xs break-all">{member.image ?? ''}</span></Table.Cell>
							<Table.Cell>
								<Badge variant={podmanStateVariant(member.state)} size="sm">{member.state}</Badge>
							</Table.Cell>
							<Table.Cell><span class="text-muted-foreground">{member.status ?? ''}</span></Table.Cell>
							<Table.Cell>
								<PortBadge
									environmentId={envId}
									ports={(member.ports ?? []).map((port) => ({
										ip: port.hostIp,
										privatePort: port.containerPort,
										publicPort: port.hostPort,
										type: port.protocol || 'tcp'
									}))}
								/>
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</Card.Root>
