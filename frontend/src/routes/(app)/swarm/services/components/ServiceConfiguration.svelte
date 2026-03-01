<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { m } from '$lib/paraglide/messages';
	import { SettingsIcon, TagIcon } from '$lib/icons';

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
			<Card.Content class="p-4">
				<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
					{#each envVars as env, index (index)}
						{#if env.includes('=')}
							{@const [key, ...valueParts] = env.split('=')}
							{@const value = valueParts.join('=')}
							<Card.Root variant="subtle">
								<Card.Content class="flex flex-col gap-2 p-4">
									<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">
										{key}
									</div>
									<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
										{value}
									</div>
								</Card.Content>
							</Card.Root>
						{:else}
							<Card.Root variant="subtle">
								<Card.Content class="flex flex-col gap-2 p-4">
									<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">{m.common_name()}</div>
									<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
										{env}
									</div>
								</Card.Content>
							</Card.Root>
						{/if}
					{/each}
				</div>
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
			<Card.Content class="p-4">
				<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
					{#each Object.entries(labels) as [key, value] (key)}
						<Card.Root variant="subtle">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">
									{key}
								</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
									{value?.toString() || ''}
								</div>
							</Card.Content>
						</Card.Root>
					{/each}
				</div>
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
			<Card.Content class="p-4">
				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
					{#if command.length > 0}
						<Card.Root variant="subtle" class="sm:col-span-2 lg:col-span-3 xl:col-span-4">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
									{m.common_command()}
								</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
									{command.join(' ')}
								</div>
							</Card.Content>
						</Card.Root>
					{/if}

					{#if args.length > 0}
						<Card.Root variant="subtle" class="sm:col-span-2 lg:col-span-3 xl:col-span-4">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
									{m.common_args()}
								</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
									{args.join(' ')}
								</div>
							</Card.Content>
						</Card.Root>
					{/if}

					{#if workingDir}
						<Card.Root variant="subtle">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
									{m.common_working_directory()}
								</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
									{workingDir}
								</div>
							</Card.Content>
						</Card.Root>
					{/if}

					{#if user}
						<Card.Root variant="subtle">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
									{m.resource_user_cap()}
								</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium select-all">
									{user}
								</div>
							</Card.Content>
						</Card.Root>
					{/if}

					{#if hostname}
						<Card.Root variant="subtle">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">{m.swarm_hostname()}</div>
								<div class="text-foreground cursor-pointer font-mono text-sm font-medium select-all">
									{hostname}
								</div>
							</Card.Content>
						</Card.Root>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>
	{/if}
</div>
