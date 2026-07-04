<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { SettingsIcon, TagIcon } from '$lib/icons';
	import { KeyValueCard, KeyValueGrid } from '$lib/components/resource-detail';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import EnvVarsList from '$lib/components/env-vars-list.svelte';

	interface Props {
		envVars: string[];
		labels: Record<string, string>;
		command: string[];
		args: string[];
		workingDir: string;
		user: string;
		hostname: string;
		hasEnvVars: boolean;
		hasLabels: boolean;
		hasAdvancedConfig: boolean;
	}

	let { envVars, labels, command, args, workingDir, user, hostname, hasEnvVars, hasLabels, hasAdvancedConfig }: Props = $props();
</script>

<DetailPanel>
	{#if hasEnvVars}
		<DetailSectionCard icon={SettingsIcon} title={m.common_environment_variables()}>
			<EnvVarsList {envVars} nameOnlyLabel={m.common_name()} />
		</DetailSectionCard>
	{/if}

	{#if hasLabels}
		<DetailSectionCard
			icon={TagIcon}
			title={m.common_labels()}
			description={m.common_labels_description({ resource: m.swarm_service() })}
		>
			<KeyValueGrid>
				{#each Object.entries(labels) as [key, value] (key)}
					<KeyValueCard label={key}>{value?.toString() || ''}</KeyValueCard>
				{/each}
			</KeyValueGrid>
		</DetailSectionCard>
	{/if}

	{#if hasAdvancedConfig}
		<DetailSectionCard icon={SettingsIcon} title={m.common_advanced()}>
			<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{#if command.length > 0}
					<div class="flex flex-col gap-1 sm:col-span-2 lg:col-span-3 xl:col-span-4">
						<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
							{m.common_command()}
						</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
							{command.join(' ')}
						</div>
					</div>
				{/if}

				{#if args.length > 0}
					<div class="flex flex-col gap-1 sm:col-span-2 lg:col-span-3 xl:col-span-4">
						<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
							{m.common_args()}
						</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
							{args.join(' ')}
						</div>
					</div>
				{/if}

				{#if workingDir}
					<div class="flex flex-col gap-1">
						<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
							{m.common_working_directory()}
						</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
							{workingDir}
						</div>
					</div>
				{/if}

				{#if user}
					<div class="flex flex-col gap-1">
						<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
							{m.resource_user_cap()}
						</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium select-all">
							{user}
						</div>
					</div>
				{/if}

				{#if hostname}
					<div class="flex flex-col gap-1">
						<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{m.swarm_hostname()}</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium select-all">
							{hostname}
						</div>
					</div>
				{/if}
			</div>
		</DetailSectionCard>
	{/if}
</DetailPanel>
