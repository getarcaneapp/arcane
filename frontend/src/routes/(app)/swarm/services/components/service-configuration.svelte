<script lang="ts">
	import EnvVarsList from '#lib/components/env-vars-list.svelte';
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { SettingsIcon, TagIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

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

<div class="space-y-6">
	{#if hasEnvVars}
		<Card.Root>
			<Card.Header icon={SettingsIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.common_environment_variables()}</h2>
					</Card.Title>
				</div>
			</Card.Header>
			<Card.Content>
				<EnvVarsList {envVars} nameOnlyLabel={m.common_name()} />
			</Card.Content>
		</Card.Root>
	{/if}

	{#if hasLabels}
		<Card.Root>
			<Card.Header icon={TagIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.common_labels()}</h2>
					</Card.Title>
					<Card.Description>{m.common_labels_description({ resource: m.swarm_service() })}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content>
				<KeyValueGrid>
					{#each Object.entries(labels) as [key, value] (key)}
						<KeyValueCard label={key}>{value?.toString() || ''}</KeyValueCard>
					{/each}
				</KeyValueGrid>
			</Card.Content>
		</Card.Root>
	{/if}

	{#if hasAdvancedConfig}
		<Card.Root>
			<Card.Header icon={SettingsIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.common_advanced()}</h2>
					</Card.Title>
				</div>
			</Card.Header>
			<Card.Content>
				<KeyValueGrid>
					{#if command.length > 0}
						<KeyValueCard label={m.common_command()} class="lg:col-span-2 2xl:col-span-3">{command.join(' ')}</KeyValueCard>
					{/if}

					{#if args.length > 0}
						<KeyValueCard label={m.common_args()} class="lg:col-span-2 2xl:col-span-3">{args.join(' ')}</KeyValueCard>
					{/if}

					{#if workingDir}
						<KeyValueCard label={m.common_working_directory()}>{workingDir}</KeyValueCard>
					{/if}

					{#if user}
						<KeyValueCard label={m.common_user()}>{user}</KeyValueCard>
					{/if}

					{#if hostname}
						<KeyValueCard label={m.hostname()}>{hostname}</KeyValueCard>
					{/if}
				</KeyValueGrid>
			</Card.Content>
		</Card.Root>
	{/if}
</div>
