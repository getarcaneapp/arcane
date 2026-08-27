<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Checkbox from '#lib/components/ui/checkbox';
	import { Spinner } from '#lib/components/ui/spinner';
	import { ArrowDownIcon, ArrowRightIcon, FileTextIcon, FolderOpenIcon, LockIcon } from '#lib/icons';
	import { cn } from '#lib/utils';

	let {
		name,
		path,
		depth,
		isDirectory,
		expanded = false,
		showDisclosure = true,
		selected = false,
		selectable = false,
		checked = false,
		indeterminate = false,
		disabled = false,
		loading = false,
		pending = false,
		locked = false,
		expandLabel,
		collapseLabel,
		lockedLabel,
		pendingLabel,
		onToggle,
		onActivate,
		onCheckedChange,
		trailing
	}: {
		name: string;
		path: string;
		depth: number;
		isDirectory: boolean;
		expanded?: boolean;
		showDisclosure?: boolean;
		selected?: boolean;
		selectable?: boolean;
		checked?: boolean;
		indeterminate?: boolean;
		disabled?: boolean;
		loading?: boolean;
		pending?: boolean;
		locked?: boolean;
		expandLabel: string;
		collapseLabel: string;
		lockedLabel?: string;
		pendingLabel?: string;
		onToggle?: () => void;
		onActivate?: () => void;
		onCheckedChange?: (checked: boolean) => void;
		trailing?: Snippet;
	} = $props();
</script>

<div
	class={cn(
		'group flex min-h-8 w-full items-center gap-1.5 rounded-md pr-2 text-[13px] hover:bg-accent',
		selected && 'bg-accent'
	)}
	style={`padding-left: ${0.5 + depth * 1}rem`}
	data-path={path}
	data-directory={isDirectory}
>
	{#if isDirectory && showDisclosure}
		<button
			type="button"
			class="inline-flex size-4 shrink-0 items-center justify-center rounded hover:bg-muted"
			aria-label={expanded ? collapseLabel : expandLabel}
			onclick={onToggle}
		>
			{#if expanded}
				<ArrowDownIcon class="size-3.5" />
			{:else}
				<ArrowRightIcon class="size-3.5" />
			{/if}
		</button>
	{:else if showDisclosure}
		<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
	{/if}

	{#if selectable}
		<Checkbox.Root
			{checked}
			{indeterminate}
			{disabled}
			aria-label={name}
			onCheckedChange={(value) => onCheckedChange?.(!!value)}
		/>
	{/if}

	<button
		type="button"
		class="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left disabled:cursor-default"
		disabled={disabled && !isDirectory}
		title={path}
		onclick={onActivate}
	>
		{#if isDirectory}
			<span class="relative size-4 shrink-0">
				<FolderOpenIcon class={cn('size-4 text-amber-500', loading && 'opacity-40')} />
				{#if loading}
					<Spinner aria-hidden="true" class="absolute inset-0 size-4 text-foreground" />
				{/if}
			</span>
		{:else}
			<FileTextIcon class="size-4 shrink-0 text-muted-foreground" />
		{/if}
		<span class="min-w-0 flex-1 truncate">{name}</span>
		{#if pending}
			<span class="size-1.5 shrink-0 rounded-full bg-primary" role="img" aria-label={pendingLabel} title={pendingLabel}></span>
		{/if}
	</button>

	{#if locked}
		<LockIcon class="mx-1 size-3.5 shrink-0 text-muted-foreground" aria-label={lockedLabel} />
	{/if}
	{@render trailing?.()}
</div>
