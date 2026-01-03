<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import * as Alert from '$lib/components/ui/alert';
	import { Switch } from '$lib/components/ui/switch';
	import { Snippet } from '$lib/components/ui/snippet';
	import { m } from '$lib/paraglide/messages';
	import type { TemplateRegistry } from '$lib/types/template.type';
	import { TrashIcon, RegistryIcon, CommunityIcon, RefreshIcon, ExternalLinkIcon, AddIcon } from '$lib/icons';

	let {
		registries,
		isLoading,
		onAddRegistry,
		onUpdateRegistry,
		onRemoveRegistry
	}: {
		registries: TemplateRegistry[];
		isLoading: {
			updating: Record<string, boolean>;
			removing: Record<string, boolean>;
		};
		onAddRegistry: () => void;
		onUpdateRegistry: (id: string, updates: { enabled?: boolean }) => void;
		onRemoveRegistry: (id: string) => void;
	} = $props();
</script>

<div class="space-y-6">
	<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h3 class="text-lg font-semibold">{m.templates_registries_section_title()}</h3>
			<p class="text-muted-foreground text-sm">{m.templates_registries_section_description()}</p>
		</div>
		<ArcaneButton action="create" onclick={onAddRegistry} class="w-full sm:w-auto">
			{m.common_add_button({ resource: m.resource_registry_cap() })}
		</ArcaneButton>
	</div>

	{#if registries.length === 0}
		<div class="space-y-4">
			<Alert.Root>
				<RegistryIcon class="size-4" />
				<Alert.Title>{m.templates_alert_remote_registries_title()}</Alert.Title>
				<Alert.Description>{m.templates_alert_remote_registries_description()}</Alert.Description>
			</Alert.Root>

			<Alert.Root class="border-primary/20 bg-primary/5 dark:border-primary/30 dark:bg-primary/10">
				<CommunityIcon class="size-4" />
				<Alert.Title>{m.templates_community_registry_title()}</Alert.Title>
				<Alert.Description class="space-y-2">
					<p>{m.templates_community_registry_description()}</p>
					<div class="flex w-full max-w-[475px] flex-col gap-2">
						<Snippet text="https://registry.getarcane.app/registry.json" />
					</div>
				</Alert.Description>
			</Alert.Root>
		</div>
	{:else}
		<div class="space-y-3">
			{#each registries as registry}
				<Card.Root class="p-4">
					<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
						<div class="min-w-0 flex-1">
							<div class="mb-1 flex flex-wrap items-center gap-2">
								<h4 class="font-medium">{registry.name}</h4>
								<Badge variant={registry.enabled ? 'default' : 'secondary'}>
									{registry.enabled ? m.common_enabled() : m.common_disabled()}
								</Badge>
							</div>
							<p class="text-muted-foreground text-sm break-all">{registry.url}</p>
							{#if registry.description}
								<p class="text-muted-foreground mt-1 text-sm">{registry.description}</p>
							{/if}
						</div>
						<div class="flex items-center gap-2 self-end sm:self-center">
							<Switch
								checked={registry.enabled}
								onCheckedChange={(checked) => onUpdateRegistry(registry.id, { enabled: checked })}
								disabled={isLoading.updating[registry.id]}
							/>

							<ArcaneButton
								action="base"
								tone="outline"
								size="sm"
								onclick={() => window.open(registry.url, '_blank', 'noopener,noreferrer')}
							>
								<ExternalLinkIcon class="size-4" />
							</ArcaneButton>

							<ArcaneButton
								action="remove"
								size="sm"
								onclick={() => onRemoveRegistry(registry.id)}
								loading={isLoading.removing[registry.id]}
							/>
						</div>
					</div>
				</Card.Root>
			{/each}
		</div>
	{/if}
</div>
