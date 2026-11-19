<script lang="ts">
	import { mergeProps } from 'bits-ui';
	import { toast } from 'svelte-sonner';

	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Command from '#lib/components/ui/command/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { AddIcon, CheckIcon, LockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ProjectTag, ProjectTagColor, ProjectTagOption } from '#lib/types/project.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

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
	const normalizedSearch = $derived(normalizeTag(search));
	const visibleTags = $derived(tags.slice(0, maxVisible));
	const overflow = $derived(Math.max(tags.length - maxVisible, 0));
	const options = $derived.by(() => {
		const byName = new Map<string, ProjectTagOption>([
			...availableTags.flatMap((tag): [string, ProjectTagOption][] => {
				const name = normalizeTag(tag.name);
				return name ? [[name, { name, color: tag.color ?? 'gray' }]] : [];
			}),
			...tags.map((tag): [string, ProjectTagOption] => [tag.name, { name: tag.name, color: tag.color ?? 'gray' }])
		]);
		return Array.from(byName.values()).sort((a, b) => a.name.localeCompare(b.name));
	});
	const canCreate = $derived(
		canEdit &&
			normalizedSearch.length > 0 &&
			isValidTag(normalizedSearch) &&
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

	function normalizeTag(value: string): string {
		return value.trim().toLowerCase();
	}

	function isValidTag(value: string): boolean {
		return value.length > 0 && Array.from(value).length <= 64 && !value.includes(',');
	}

	function isComposeTag(tag: ProjectTag | undefined): boolean {
		return tag?.sources.includes('compose') ?? false;
	}

	function tagColorClass(color: ProjectTagColor | undefined): string {
		switch (color) {
			case 'purple':
				return 'bg-violet';
			case 'blue':
				return 'bg-info';
			case 'green':
				return 'bg-success';
			case 'yellow':
				return 'bg-warning';
			case 'orange':
				return 'bg-orange';
			case 'red':
				return 'bg-destructive';
			case 'pink':
				return 'bg-pink';
			default:
				return 'bg-muted-foreground';
		}
	}

	async function toggleTag(name: string, color: ProjectTagColor = 'gray') {
		const normalized = normalizeTag(name);
		if (!canEdit || !isValidTag(normalized) || pending) return;
		const existing = tags.find((tag) => tag.name === normalized);
		if (isComposeTag(existing)) return;

		const previous = tags;
		const attached = !existing;
		const resolvedColor = existing?.color ?? color;
		if (attached) {
			const added: ProjectTag = { name: normalized, color: resolvedColor, sources: ['ui'] };
			tags = [...tags, added].sort((a, b) => a.name.localeCompare(b.name));
		} else {
			tags = tags.filter((tag) => tag.name !== normalized);
		}
		pending = normalized;
		try {
			const toggled = await tryCatch(Promise.resolve().then(() => onToggle?.(normalized, attached, resolvedColor)));
			if (toggled.error !== null) {
				tags = previous;
				toast.error(toggled.error instanceof Error ? toggled.error.message : m.common_error());
				return;
			}
			if (toggled.data) tags = toggled.data;
			search = '';
			selectedColor = 'gray';
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
						variant={isComposeTag(tag) ? 'gray' : 'outline'}
						size="sm"
						title={isComposeTag(tag) ? m.project_tags_defined_in_compose() : undefined}
					>
						<span class="size-2 shrink-0 rounded-full {tagColorClass(tag.color)}"></span>
						{tag.name}
						{#if isComposeTag(tag)}<LockIcon class="size-3" />{/if}
					</Badge>
				{/each}
				{#if overflow > 0}<Badge variant="gray" size="sm">+{overflow}</Badge>{/if}
				{#if canEdit}<AddIcon class="size-4 shrink-0 text-muted-foreground" aria-label={m.project_tags_add()} />{/if}
			</button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-72" align="start" onclick={(event) => event.stopPropagation()}>
		<Command.Root>
			<Command.Input bind:value={search} placeholder={m.project_tags_search()} />
			<Command.List>
				<Command.Empty>
					{#if canCreate}
						<div class="flex flex-col items-center gap-4 px-4">
							<p class="leading-snug">
								{m.project_tags_create_prompt()}<br />
								<span class="font-medium">“{normalizedSearch}”</span>
							</p>
							<div class="flex items-center gap-2">
								<Select.Root type="single" bind:value={selectedColor}>
									<Select.Trigger class="h-9 min-w-28">
										<span class="block size-2.5 shrink-0 rounded-full {tagColorClass(selectedColor)}"></span>
										{colorOptions.find((option) => option.value === selectedColor)?.label}
									</Select.Trigger>
									<Select.Content>
										{#each colorOptions as option (option.value)}
											<Select.Item value={option.value}>
												<span class="block size-2.5 shrink-0 rounded-full {tagColorClass(option.value)}"></span>
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
										void toggleTag(normalizedSearch, selectedColor);
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
						{@const locked = isComposeTag(tag)}
						<Command.Item
							disabled={!canEdit || locked || pending !== null}
							onSelect={() => toggleTag(option.name, option.color)}
							title={locked ? m.project_tags_defined_in_compose() : undefined}
						>
							<span class="flex size-4 items-center justify-center rounded-sm border">
								{#if tag}<CheckIcon class="size-4" />{/if}
							</span>
							<span class="size-2.5 shrink-0 rounded-full {tagColorClass(option.color)}"></span>
							<span class="min-w-0 flex-1 truncate">{option.name}</span>
							{#if locked}<LockIcon class="size-4" /><span class="sr-only">{m.project_tags_defined_in_compose()}</span>{/if}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>
