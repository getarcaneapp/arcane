<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import TagIcon from '@lucide/svelte/icons/tag';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/container.type';

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
					<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
						{#each container.config.env as env, index (index)}
							{#if env.includes('=')}
								{@const [key, ...valueParts] = env.split('=')}
								{@const value = valueParts.join('=')}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{key}</div>
										<div
											class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
											title="Click to select"
										>
											{value}
										</div>
									</Card.Content>
								</Card.Root>
							{:else}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">ENV_VAR</div>
										<div
											class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
											title="Click to select"
										>
											{env}
										</div>
									</Card.Content>
								</Card.Root>
							{/if}
						{/each}
					</div>
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
					<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
						{#each Object.entries(container.labels) as [key, value] (key)}
							<Card.Root variant="subtle">
								<Card.Content class="flex flex-col gap-2 p-4">
									<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{key}</div>
									<div
										class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
										title="Click to select"
									>
										{value?.toString() || ''}
									</div>
								</Card.Content>
							</Card.Root>
						{/each}
					</div>
				{:else}
					<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
						<div class="text-sm">{m.containers_no_labels_defined()}</div>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	{/if}
</div>
