<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/container.type';
	import { SettingsIcon, TagIcon } from '$lib/icons';
	import { KeyValueCard, KeyValueGrid } from '$lib/components/resource-detail';

	interface Props {
		container: ContainerDetailsDto;
		hasEnvVars: boolean;
		hasLabels: boolean;
	}

	let { container, hasEnvVars, hasLabels }: Props = $props();
</script>

<div class="space-y-6">
	{#if hasEnvVars}
		<Card.Root>
			<Card.Header icon={SettingsIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>
							{m.common_environment_variables()}
						</h2>
					</Card.Title>
					<Card.Description>{m.containers_env_vars_description()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="p-4">
				{#if container.config?.env && container.config.env.length > 0}
					<KeyValueGrid>
						{#each container.config.env as env, index (index)}
							{#if env.includes('=')}
								{@const [key, ...valueParts] = env.split('=')}
								{@const value = valueParts.join('=')}
								<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value}</KeyValueCard>
							{:else}
								<KeyValueCard
									label={m.swarm_service_env_var()}
									labelClass="text-muted-foreground text-xs font-semibold tracking-wide uppercase"
									valueTitle={m.common_click_to_select()}
								>
									{env}
								</KeyValueCard>
							{/if}
						{/each}
					</KeyValueGrid>
				{:else}
					<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
						<div class="text-sm">{m.containers_no_env_vars()}</div>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	{/if}

	{#if hasLabels}
		<Card.Root>
			<Card.Header icon={TagIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>
							{m.common_labels()}
						</h2>
					</Card.Title>
					<Card.Description>{m.common_labels_description({ resource: m.resource_container() })}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="p-4">
				{#if container.labels && Object.keys(container.labels).length > 0}
					<KeyValueGrid>
						{#each Object.entries(container.labels) as [key, value] (key)}
							<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value?.toString() || ''}</KeyValueCard>
						{/each}
					</KeyValueGrid>
				{:else}
					<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
						<div class="text-sm">{m.containers_no_labels_defined()}</div>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	{/if}
</div>
