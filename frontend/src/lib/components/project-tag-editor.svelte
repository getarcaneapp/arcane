<script lang="ts">
	import type { ProjectTag, ProjectTagColor, ProjectTagOption } from '#lib/types/swarm';
	import * as Command from '#lib/components/ui/command';
	import * as Popover from '#lib/components/ui/popover';
	import * as Select from '#lib/components/ui/select';
	import { Badge } from '#lib/components/ui/badge';
	import { AddIcon, CheckIcon, LockIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { mergeProps } from 'bits-ui';

	let {
		tags = $bindable([]),
		availableTags = [],
		canEdit = true,
		maxVisible = 3,
		onToggle,
		class: className = ''
	}: {
		tags?: ProjectTag[];
		availableTags?: ProjectTagOption[];
		canEdit?: boolean;
		maxVisible?: number;
		onToggle?: (name: string, attached: boolean, color: ProjectTagColor) => Promise<ProjectTag[] | void> | ProjectTag[] | void;
		class?: string;
	} = $props();

	let search = $state('');
	let pending = $state<string | null>(null);
	let selectedColor = $state<ProjectTagColor>('gray');
	const normalizedSearch = $derived(normalizeTagInternal(search));
	const visibleTags = $derived(tags.slice(0, maxVisible));
	const overflow = $derived(Math.max(tags.length - maxVisible, 0));
	const options = $derived.by(() => {
		const byName = new Map<string, ProjectTagOption>();
		for (const tag of availableTags) {
			const name = normalizeTagInternal(tag.name);
			if (name) byName.set(name, { name, color: tag.color ?? 'gray' });
		}
		for (const tag of tags) {
			byName.set(tag.name, { name: tag.name, color: tag.color ?? 'gray' });
		}
		return Array.from(byName.values()).sort((a, b) => a.name.localeCompare(b.name));
	});
	const canCreate = $derived(
		canEdit &&
			normalizedSearch.length > 0 &&
			isValidTagInternal(normalizedSearch) &&
			!options.some((option) => option.name === normalizedSearch)
	);
	const colorOptions = $derived([
		{ value: 'gray' as const, label: m.project_tags_color_gray() },
		{ value: 'purple' as const, label: m.project_tags_color_purple() },
		{ value: 'blue' as const, label: m.project_tags_color_blue() },
		{ value: 'green' as const, label: m.project_tags_color_green() },
		{ value: 'yellow' as const, label: m.project_tags_color_yellow() },
		{ value: 'orange' as const, label: m.project_tags_color_orange() },
		{ value: 'red' as const, label: m.project_tags_color_red() },
		{ value: 'pink' as const, label: m.project_tags_color_pink() }
	]);

	function normalizeTagInternal(value: string): string {
		return value.trim().toLowerCase();
	}

	function isValidTagInternal(value: string): boolean {
		return (
			value.length > 0 && Array.from(value).length <= 64 && !value.includes(',') && !/[\u0000-\u001f\u007f-\u009f]/u.test(value)
		);
	}

	function isComposeTagInternal(tag: ProjectTag | undefined): boolean {
		return tag?.sources.includes('compose') ?? false;
	}

	function tagColorClassInternal(color: ProjectTagColor | undefined): string {
		switch (color) {
			case 'purple':
				return 'bg-violet-500';
			case 'blue':
				return 'bg-blue-500';
			case 'green':
				return 'bg-emerald-500';
			case 'yellow':
				return 'bg-amber-400';
			case 'orange':
				return 'bg-orange-500';
			case 'red':
				return 'bg-red-500';
			case 'pink':
				return 'bg-pink-500';
			default:
				return 'bg-zinc-400';
		}
	}

	async function toggleTagInternal(name: string, color: ProjectTagColor = 'gray') {
		const normalized = normalizeTagInternal(name);
		if (!canEdit || !isValidTagInternal(normalized) || pending) return;
		const existing = tags.find((tag) => tag.name === normalized);
		if (isComposeTagInternal(existing)) return;

		const previous = tags;
		const attached = !existing;
		const resolvedColor = existing?.color ?? color;
		tags = attached
			? [...tags, { name: normalized, color: resolvedColor, sources: ['ui'] } satisfies ProjectTag].sort((a, b) =>
					a.name.localeCompare(b.name)
				)
			: tags.filter((tag) => tag.name !== normalized);
		pending = normalized;
		try {
			const updated = await onToggle?.(normalized, attached, resolvedColor);
			if (updated) tags = updated;
			search = '';
			selectedColor = 'gray';
		} catch (error) {
			tags = previous;
			toast.error(error instanceof Error ? error.message : m.common_error());
		} finally {
			pending = null;
		}
	}
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}
			{@const triggerProps = mergeProps(props, {
				onclick: (event: MouseEvent) => event.stopPropagation()
			})}
			<button {...triggerProps} type="button" class="flex min-h-7 max-w-full items-center gap-1 text-left {className}">
				{#each visibleTags as tag (tag.name)}
					<Badge
						variant={isComposeTagInternal(tag) ? 'gray' : 'outline'}
						size="sm"
						title={isComposeTagInternal(tag) ? m.project_tags_defined_in_compose() : undefined}
					>
						<span class="size-2 shrink-0 rounded-full {tagColorClassInternal(tag.color)}"></span>
						{tag.name}
						{#if isComposeTagInternal(tag)}<LockIcon class="size-3" />{/if}
					</Badge>
				{/each}
				{#if overflow > 0}<Badge variant="gray" size="sm">+{overflow}</Badge>{/if}
				{#if canEdit}<AddIcon class="size-4 shrink-0 text-muted-foreground" aria-label={m.project_tags_add()} />{/if}
			</button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-72 p-0" align="start" onclick={(event) => event.stopPropagation()}>
		<Command.Root class="rounded-none bg-transparent">
			<Command.Input bind:value={search} placeholder={m.project_tags_search()} />
			<Command.List>
				<Command.Empty class={canCreate ? 'px-4 py-5' : undefined}>
					{#if canCreate}
						<div class="flex flex-col items-center gap-4">
							<p class="leading-snug">
								{m.project_tags_create_prompt()}<br />
								<span class="font-medium">“{normalizedSearch}”</span>
							</p>
							<div class="flex items-center gap-2">
								<Select.Root type="single" bind:value={selectedColor}>
									<Select.Trigger class="h-9 min-w-28 bg-background">
										<span class="block size-2.5 shrink-0 rounded-full {tagColorClassInternal(selectedColor)}"></span>
										{colorOptions.find((option) => option.value === selectedColor)?.label}
									</Select.Trigger>
									<Select.Content>
										{#each colorOptions as option (option.value)}
											<Select.Item value={option.value}>
												<span class="block size-2.5 shrink-0 rounded-full {tagColorClassInternal(option.value)}"></span>
												{option.label}
											</Select.Item>
										{/each}
									</Select.Content>
								</Select.Root>
								<button
									type="button"
									class="h-9 rounded-md border border-input bg-background px-3 text-sm font-medium shadow-xs hover:bg-accent disabled:opacity-50"
									disabled={pending !== null}
									onclick={(event) => {
										event.stopPropagation();
										void toggleTagInternal(normalizedSearch, selectedColor);
									}}
								>
									{m.common_create()}
								</button>
							</div>
						</div>
					{:else}
						{m.common_no_results_found()}
					{/if}
				</Command.Empty>
				<Command.Group>
					{#each options as option (option.name)}
						{@const tag = tags.find((item) => item.name === option.name)}
						{@const locked = isComposeTagInternal(tag)}
						<Command.Item
							disabled={!canEdit || locked || pending !== null}
							onSelect={() => toggleTagInternal(option.name, option.color)}
							title={locked ? m.project_tags_defined_in_compose() : undefined}
						>
							<span class="flex size-4 items-center justify-center rounded-sm border">
								{#if tag}<CheckIcon class="size-4" />{/if}
							</span>
							<span class="size-2.5 shrink-0 rounded-full {tagColorClassInternal(option.color)}"></span>
							<span class="min-w-0 flex-1 truncate">{option.name}</span>
							{#if locked}<LockIcon class="size-4" /><span class="sr-only">{m.project_tags_defined_in_compose()}</span>{/if}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>
