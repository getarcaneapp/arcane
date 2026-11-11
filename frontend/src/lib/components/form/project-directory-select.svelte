<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { Select as SelectPrimitive } from 'bits-ui';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { ArrowDownIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectDirectoriesQuery } from '#lib/query/projects.js';

	let {
		value = $bindable(''),
		environmentId,
		readonly = false
	}: {
		value?: string;
		environmentId: string;
		readonly?: boolean;
	} = $props();

	const directoriesQuery = createQuery(() => projectDirectoriesQuery(environmentId));
	const directories = $derived(directoriesQuery.data ?? []);
	const options = $derived(directories.filter((directory) => directory.enabled));
	const primary = $derived(directories.find((directory) => directory.primary));
	const current = $derived(options.find((directory) => directory.containerPath === value) ?? primary);

	function pick(containerPath: string) {
		value = containerPath === primary?.containerPath ? '' : containerPath;
	}
</script>

{#snippet prefixSeparator()}
	<span class="text-sm text-muted-foreground/60">/</span>
{/snippet}

{#if readonly}
	{#if directories.length > 1 && value}
		<div class="flex min-w-0 items-center gap-2 pr-2 text-sm text-muted-foreground sm:shrink-0" title={value}>
			<span class="inline-flex min-w-0 items-center gap-1.5">
				<FolderOpenIcon class="size-4 shrink-0" />
				<span class="max-w-24 truncate font-mono sm:max-w-48">{value}</span>
			</span>
			{@render prefixSeparator()}
		</div>
	{/if}
{:else if options.length > 1 && current}
	<Label for="project-directory" class="sr-only">{m.project_directory()}</Label>
	<div class="flex shrink-0 items-center gap-1 pr-2">
		<Select.Root type="single" bind:value={() => current.containerPath, pick}>
			<SelectPrimitive.Trigger id="project-directory">
				{#snippet child({ props })}
					<ArcaneButton {...props} action="base" tone="ghost" title={current.path} class="shrink-0">
						<FolderOpenIcon class="size-4 text-muted-foreground" />
						<span class="hidden max-w-48 truncate font-mono sm:inline">{current.containerPath}</span>
						<ArrowDownIcon class="size-3.5 opacity-60" />
					</ArcaneButton>
				{/snippet}
			</SelectPrimitive.Trigger>
			<Select.Content class="w-auto max-w-72 min-w-48">
				{#each options as directory (directory.containerPath)}
					<Select.Item value={directory.containerPath} label={directory.containerPath}>
						<FolderOpenIcon class="size-4 text-muted-foreground" />
						<span class="truncate font-mono">{directory.containerPath}</span>
					</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		{@render prefixSeparator()}
	</div>
{/if}
