<script lang="ts">
	import { DetailSection } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import {
		DockerBrandIcon,
		ExternalLinkIcon,
		JobsIcon,
		LayersIcon,
		NetworksIcon,
		TagIcon,
		UpdateIcon,
		ZapIcon,
		type IconType
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import {
		parseWellKnownLabels,
		type LabelField,
		type LabelGroupId,
		type LabelResource,
		type LabelResourceKind
	} from '#lib/utils/well-known-labels.js';

	import LabelValue from './label-value.svelte';

	interface Props {
		labels: Record<string, string> | null | undefined;
	}

	let { labels }: Props = $props();

	const iconCatalog = $derived(userStore.current?.preferences?.iconCatalog ?? 'selfhst');
	const parsed = $derived(parseWellKnownLabels(labels, iconCatalog));

	const GROUP_META: Record<LabelGroupId, { title: () => string; icon: IconType }> = {
		traefik: { title: m.traefik, icon: NetworksIcon },
		arcane: { title: m.layout_title, icon: ZapIcon },
		oci: { title: m.oci_image_metadata, icon: LayersIcon },
		ofelia: { title: m.ofelia, icon: JobsIcon },
		watchtower: { title: m.watchtower, icon: UpdateIcon },
		compose: { title: m.common_docker_compose, icon: DockerBrandIcon }
	};

	const RESOURCE_KIND_LABELS: Partial<Record<LabelResourceKind, () => string>> = {
		router: m.router,
		service: m.swarm_service,
		middleware: m.middleware
	};
</script>

{#snippet fieldRow(field: LabelField, widthClass: string)}
	<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:gap-6">
		<dt class="shrink-0 text-xs break-all text-muted-foreground {widthClass}" title={field.rawKey}>
			{field.label}
		</dt>
		<dd class="min-w-0 flex-1">
			<LabelValue value={field.value} />
		</dd>
	</div>
{/snippet}

{#snippet fieldRows(fields: LabelField[])}
	<dl class="divide-y divide-border/40">
		{#each fields as field (field.rawKey)}
			{@render fieldRow(field, 'sm:w-56')}
		{/each}
	</dl>
{/snippet}

{#snippet resourceSection(resource: LabelResource)}
	{@const kindLabel = RESOURCE_KIND_LABELS[resource.kind]}
	<div class="space-y-1 border-l-2 border-border pl-4">
		<div class="flex flex-wrap items-center gap-2 pb-1">
			<span class="font-mono text-sm font-medium break-all text-foreground">{resource.name}</span>
			{#if kindLabel}
				<Badge variant="secondary" size="xs">{kindLabel()}</Badge>
			{/if}
			{#if resource.meta.length > 0}
				<span class="font-mono text-xs text-muted-foreground">{resource.meta.join(' · ')}</span>
			{/if}
		</div>

		<dl class="divide-y divide-border/40">
			{#if resource.codeBlock}
				<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:gap-6">
					<dt class="shrink-0 text-xs text-muted-foreground sm:w-52" title={resource.codeBlock.rawKey}>
						{resource.codeBlock.kind === 'rule' ? m.rule() : m.common_command()}
					</dt>
					<dd class="min-w-0 flex-1 space-y-1">
						<code
							class="block cursor-pointer font-mono text-sm break-all text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{resource.codeBlock.value}
						</code>
						{#if resource.codeBlock.links && resource.codeBlock.links.length > 0}
							<div class="flex flex-wrap items-center gap-x-4 gap-y-1">
								{#each resource.codeBlock.links as link (link.label)}
									{#if link.href}
										<a
											href={link.href}
											target="_blank"
											rel="external noopener noreferrer"
											class="inline-flex items-center gap-1 font-mono text-xs text-primary-text hover:underline"
										>
											<span>{link.label}</span>
											<ExternalLinkIcon class="size-3 shrink-0" />
										</a>
									{:else}
										<span class="font-mono text-xs text-muted-foreground">{link.label}</span>
									{/if}
								{/each}
							</div>
						{/if}
					</dd>
				</div>
			{/if}

			{#each resource.fields as field (field.rawKey)}
				{@render fieldRow(field, 'sm:w-52')}
			{/each}
		</dl>
	</div>
{/snippet}

<div class="space-y-6">
	{#each parsed.groups as group (group.id)}
		{@const meta = GROUP_META[group.id]}
		<DetailSection title={meta.title()} icon={meta.icon} class="first:border-t-0 first:pt-0">
			<div class="space-y-5">
				{#if group.fields.length > 0}
					{@render fieldRows(group.fields)}
				{/if}

				{#each group.resources as resource (resource.id)}
					{@render resourceSection(resource)}
				{/each}
			</div>
		</DetailSection>
	{/each}

	{#if parsed.other.length > 0}
		<DetailSection
			title={parsed.groups.length > 0 ? m.other_labels() : m.common_labels()}
			icon={TagIcon}
			class="first:border-t-0 first:pt-0"
		>
			<dl class="divide-y divide-border/40">
				{#each parsed.other as entry (entry.key)}
					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:gap-6">
						<dt class="shrink-0 font-mono text-xs break-all text-muted-foreground sm:w-56">
							{entry.key}
						</dt>
						<dd
							class="min-w-0 flex-1 cursor-pointer font-mono text-sm break-all text-foreground select-all"
							title={m.common_click_to_select()}
						>
							{entry.value}
						</dd>
					</div>
				{/each}
			</dl>
		</DetailSection>
	{/if}
</div>
