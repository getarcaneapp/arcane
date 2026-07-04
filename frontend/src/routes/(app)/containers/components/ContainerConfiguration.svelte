<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { SettingsIcon, TagIcon } from '$lib/icons';
	import { KeyValueCard, KeyValueGrid } from '$lib/components/resource-detail';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import EnvVarsList from '$lib/components/env-vars-list.svelte';

	interface Props {
		container: ContainerDetailsDto;
		hasEnvVars: boolean;
		hasLabels: boolean;
	}

	let { container, hasEnvVars, hasLabels }: Props = $props();
</script>

<!-- fallow-ignore-next-line code-duplication container vs swarm-service config; typed props diverge across the boundary -->
<DetailPanel>
	{#if hasEnvVars}
		<DetailSectionCard
			icon={SettingsIcon}
			title={m.common_environment_variables()}
			description={m.containers_env_vars_description()}
		>
			{#if container.config?.env && container.config.env.length > 0}
				<EnvVarsList
					envVars={container.config.env}
					nameOnlyLabel={m.swarm_service_env_var()}
					valueTitle={m.common_click_to_select()}
				/>
			{:else}
				<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
					<div class="text-sm">{m.containers_no_env_vars()}</div>
				</div>
			{/if}
			<!-- fallow-ignore-next-line code-duplication container vs swarm-service config; typed props diverge across the boundary -->
		</DetailSectionCard>
	{/if}

	{#if hasLabels}
		<DetailSectionCard
			icon={TagIcon}
			title={m.common_labels()}
			description={m.common_labels_description({ resource: m.resource_container() })}
		>
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
		</DetailSectionCard>
	{/if}
</DetailPanel>
