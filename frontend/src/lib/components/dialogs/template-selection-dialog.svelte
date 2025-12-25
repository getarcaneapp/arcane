<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Card } from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import type { Template } from '$lib/types/template.type';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import {
		ArrowDownIcon,
		ArrowRightIcon,
		RegistryIcon,
		ProjectsIcon,
		DownloadIcon,
		SettingsIcon,
		FileTextIcon
	} from '$lib/icons';

	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';

	interface Props {
		open: boolean;
		templates?: Template[];
		onSelect: (template: Template) => void;
		onDownloadSuccess?: () => void;
	}

	let { open = $bindable(), templates = [], onSelect, onDownloadSuccess }: Props = $props();

	let loadingStates = $state<Set<string>>(new Set());
	let sortBy = $state<'name-asc' | 'name-desc'>('name-asc');
	let groupByRegistry = $state(true);

	const allTemplates = $derived(templates ?? []);

	const sortedTemplates = $derived.by(() => {
		const sorted = [...allTemplates];
		sorted.sort((a, b) => (sortBy === 'name-asc' ? a.name.localeCompare(b.name) : b.name.localeCompare(a.name)));
		return sorted;
	});

	const groupedTemplates = $derived.by(() => {
		if (!groupByRegistry) return [];

		const groups = new Map<string, Template[]>();
		for (const template of sortedTemplates) {
			const key = template.registry?.name ?? (template.isRemote ? m.templates_remote() : m.templates_local());
			const items = groups.get(key) ?? [];
			items.push(template);
			groups.set(key, items);
		}

		return Array.from(groups.entries())
			.map(([name, items]) => ({ name, items }))
			.sort((a, b) => a.name.localeCompare(b.name));
	});

	const filters = {
		'name-asc': m.templates_sort_name_asc(),
		'name-desc': m.templates_sort_name_desc()
	};

	function normalizeTags(tags: unknown): string[] {
		if (!tags || tags === null || tags === undefined) return [];

		let list: unknown = tags;
		if (typeof tags === 'string') {
			const trimmed = tags.trim();
			if (!trimmed) return [];
			if (trimmed.startsWith('[')) {
				try {
					list = JSON.parse(trimmed);
				} catch {
					list = trimmed.split(',');
				}
			} else {
				list = trimmed.split(',');
			}
		}

		if (!Array.isArray(list)) return [];

		return list
			.map((t) => {
				if (t === null || t === undefined) return '';
				return String(t)
					.trim()
					.replace(/^["']|["']$/g, '');
			})
			.filter(Boolean)
			.map((t) => t.charAt(0).toUpperCase() + t.slice(1));
	}

	async function handleSelect(template: Template) {
		const loadingKey = template.id;
		loadingStates.add(loadingKey);

		try {
			const details = await templateService.getTemplateContent(template.id);
			if (!details) {
				toast.error(m.templates_load_failed());
				return;
			}

			onSelect({
				...details.template,
				content: details.content,
				envContent: details.envContent
			});
			open = false;
			toast.success(m.templates_loaded_success({ name: template.name }));
		} catch (error) {
			console.error('Error loading template:', error);
			toast.error(error instanceof Error ? error.message : m.templates_load_failed());
		} finally {
			loadingStates.delete(loadingKey);
		}
	}

	async function handleDownload(template: Template) {
		if (!template.isRemote) return;

		const loadingKey = `download-${template.id}`;
		loadingStates.add(loadingKey);

		try {
			const result = await templateService.download(template.id);
			if (result) {
				toast.success(m.templates_downloaded_success({ name: template.name }));
				onDownloadSuccess?.();
			} else {
				toast.error(m.templates_download_failed());
			}
		} catch (error) {
			console.error('Error downloading template:', error);
			toast.error(error instanceof Error ? error.message : m.templates_download_failed());
		} finally {
			loadingStates.delete(loadingKey);
		}
	}
</script>

{#snippet templateCard(template: Template, showRegistry: boolean = false)}
	<Card class="hover:bg-muted/50 hover:border-primary/20 border transition-colors">
		<div class="p-4">
			<div class="mb-2 flex items-start justify-between gap-2">
				<h4 class="truncate pr-2 font-semibold">{template.name}</h4>
				<div class="ml-2 flex flex-shrink-0 flex-wrap items-center gap-1">
					{#if template.metadata?.version}
						<Badge variant="outline" class="text-xs">v{template.metadata.version}</Badge>
					{/if}
					{#if template.metadata?.envUrl || template.envContent}
						<Badge variant="secondary" class="text-xs">
							<SettingsIcon class="mr-1 size-3" />
							ENV
						</Badge>
					{/if}
				</div>
			</div>

			{#if showRegistry}
				<div class="mb-2">
					<Badge variant="secondary" class="text-xs">
						{#if template.isRemote}
							<RegistryIcon class="size-3" />
						{:else}
							<ProjectsIcon class="size-3" />
						{/if}
						{template.registry?.name ?? (template.isRemote ? m.templates_remote() : m.templates_local())}
					</Badge>
				</div>
			{/if}

			<p class="text-muted-foreground mb-3 line-clamp-2 text-sm">
				{template.description}
			</p>

			{#if normalizeTags(template.metadata?.tags).length > 0}
				<div class="mb-3 flex flex-wrap gap-1">
					{#each normalizeTags(template.metadata?.tags) as tag}
						<Badge variant="outline" class="text-[10px]">{tag}</Badge>
					{/each}
				</div>
			{/if}

			<div class="flex items-center justify-between gap-2">
				<div class="text-muted-foreground text-xs">
					{template.isRemote ? m.templates_remote_template_label() : m.templates_local_template_label()}
				</div>
				<div class="flex gap-2">
					{#if template.isRemote}
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							onclick={() => handleDownload(template)}
							disabled={loadingStates.has(`download-${template.id}`)}
							loading={loadingStates.has(`download-${template.id}`)}
							icon={DownloadIcon}
							customLabel={m.templates_download()}
							loadingLabel={m.common_action_downloading()}
						/>
					{/if}
					<ArcaneButton
						action="base"
						size="sm"
						onclick={() => handleSelect(template)}
						disabled={loadingStates.has(template.id)}
						loading={loadingStates.has(template.id)}
						customLabel={m.templates_use_now()}
						loadingLabel={m.common_loading()}
					/>
				</div>
			</div>
		</div>
	</Card>
{/snippet}

<ResponsiveDialog
	bind:open
	title={m.templates_choose_title()}
	description={m.templates_choose_description()}
	contentClass="sm:max-w-[900px]"
>
	{#snippet children()}
		<div class="space-y-4">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<div class="flex items-center gap-3">
					<SwitchWithLabel
						id="groupByRegistrySwitch"
						label={m.templates_group_by_registry_label()}
						description={m.templates_group_by_registry_description()}
						bind:checked={groupByRegistry}
					/>
				</div>
				<div class="flex items-center gap-3">
					<Label for="sortBy" class="text-sm font-medium whitespace-nowrap">{m.common_sort_by()}</Label>
					<Select.Root bind:value={sortBy} type="single">
						<Select.Trigger id="sortBy" class="bg-background h-9 rounded-md border px-2 text-sm">
							{filters[sortBy]}
						</Select.Trigger>
						<Select.Content>
							{#each Object.entries(filters) as [value, label]}
								<Select.Item {value}>{label}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
			</div>

			<ScrollArea class="max-h-[65vh]">
				{#if allTemplates.length === 0}
					<div class="text-muted-foreground py-10 text-center">
						<FileTextIcon class="mx-auto mb-4 size-12 opacity-50" />
						<p class="mb-2">{m.templates_no_templates()}</p>
						<p class="text-sm">
							{m.templates_add_registry_prompt_part1()}
							<a href="/customize/templates" class="text-primary hover:underline">{m.templates_template_settings()}</a>
							{m.templates_add_registry_prompt_part2()}
						</p>
					</div>
				{:else if groupByRegistry && groupedTemplates.length > 0}
					<div class="space-y-3">
						{#each groupedTemplates as group}
							<Collapsible.Root class="w-full">
								<Card class="border-2">
									<Collapsible.Trigger class="flex w-full items-center justify-between px-4 py-3 text-left">
										<div class="flex items-center gap-2">
											<ArrowDownIcon class="hidden size-4 data-[state=open]:block" />
											<ArrowRightIcon class="block size-4 data-[state=open]:hidden" />
											<span class="font-semibold">{group.name}</span>
											<Badge variant="secondary" class="ml-2">{group.items.length}</Badge>
										</div>
									</Collapsible.Trigger>
									<Collapsible.Content>
										<div class="px-6 pb-6">
											<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
												{#each group.items as template}
													{@render templateCard(template)}
												{/each}
											</div>
										</div>
									</Collapsible.Content>
								</Card>
							</Collapsible.Root>
						{/each}
					</div>
				{:else}
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						{#each sortedTemplates as template}
							{@render templateCard(template, true)}
						{/each}
					</div>
				{/if}
			</ScrollArea>
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
	{/snippet}
</ResponsiveDialog>

<style>
	.line-clamp-2 {
		display: -webkit-box;
		line-clamp: 2;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
</style>
