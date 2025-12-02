<script lang="ts">
	import { goto } from '$app/navigation';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import { toast } from 'svelte-sonner';
	import AddTemplateRegistrySheet from '$lib/components/sheets/add-template-registry-sheet.svelte';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import * as Tabs from '$lib/components/ui/tabs';
	import TemplatesBrowser from './components/TemplatesBrowser.svelte';
	import RegistryManager from './components/RegistryManager.svelte';
	import type { TemplateRegistry } from '$lib/types/template.type';
	import { untrack } from 'svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

	let { data } = $props();

	let templates = $state(untrack(() => data.templates));
	let registries = $state<TemplateRegistry[]>(untrack(() => data.registries));
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.templateRequestOptions));
	let activeView = $state<'browse' | 'registries'>('browse');

	const tabItems: TabItem[] = [
		{
			value: 'browse',
			label: m.templates_browse_templates(),
			icon: LayersIcon
		},
		{
			value: 'registries',
			label: m.templates_manage_registries(),
			icon: GlobeIcon
		}
	];

	let isLoading = $state({
		addingRegistry: false,
		removing: {} as Record<string, boolean>,
		updating: {} as Record<string, boolean>
	});

	let showAddRegistrySheet = $state(false);

	async function updateRegistry(id: string, updates: { enabled?: boolean }) {
		if (isLoading.updating[id]) return;
		isLoading.updating[id] = true;

		try {
			const registry = registries.find((r) => r.id === id);
			if (!registry) {
				toast.error(m.templates_registry_not_found());
				delete isLoading.updating[id];
				return;
			}

			await templateService.updateRegistry(id, {
				name: registry.name,
				url: registry.url,
				description: registry.description,
				enabled: updates.enabled ?? registry.enabled
			});

			registries = await templateService.getRegistries();
			templates = await templateService.getTemplates(requestOptions);
			toast.success(m.common_update_success({ resource: m.resource_registry() }));
		} catch (error) {
			console.error('Error updating registry:', error);
			toast.error(error instanceof Error ? error.message : m.registries_save_failed());
		} finally {
			delete isLoading.updating[id];
		}
	}

	async function removeRegistry(id: string) {
		if (isLoading.removing[id]) return;
		isLoading.removing[id] = true;

		try {
			const reg = registries.find((r) => r.id === id);
			await templateService.deleteRegistry(id);
			registries = registries.filter((r) => r.id !== id);
			registries = await templateService.getRegistries();
			templates = await templateService.getTemplates(requestOptions);
			toast.success(
				reg
					? m.common_delete_success({ resource: `${m.resource_registry()} "${reg.url}"` })
					: m.templates_registry_removed_success()
			);
		} catch (error) {
			console.error('Error removing registry:', error);
			toast.error(error instanceof Error ? error.message : m.registries_save_failed());
		} finally {
			delete isLoading.removing[id];
		}
	}

	async function refreshTemplates() {
		try {
			templates = await templateService.getTemplates(requestOptions);
			toast.success(m.templates_refreshed());
		} catch (error) {
			console.error('Error refreshing templates:', error);
			toast.error(m.common_refresh_failed({ resource: m.templates_title() }));
		}
	}

	async function handleRegistrySubmit(registry: { name: string; url: string; description?: string; enabled: boolean }) {
		isLoading.addingRegistry = true;

		try {
			await templateService.addRegistry({
				name: registry.name.trim(),
				url: registry.url.trim(),
				description: registry.description?.trim() || undefined,
				enabled: registry.enabled
			});

			registries = await templateService.getRegistries();
			templates = await templateService.getTemplates(requestOptions);
			showAddRegistrySheet = false;

			toast.success(m.common_create_success({ resource: m.resource_registry() }));
		} catch (error) {
			console.error('Error adding registry:', error);
			toast.error(error instanceof Error ? error.message : m.registries_save_failed());
		} finally {
			isLoading.addingRegistry = false;
		}
	}

	const actionButtons: ActionButton[] = [
		{
			id: 'default',
			action: 'edit',
			label: m.templates_edit_default(),
			onclick: () => goto('/customize/templates/default')
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshTemplates
		}
	];

	const localTemplatesCount = $derived(templates.data?.filter((t) => !t.isRemote).length ?? 0);
	const remoteTemplatesCount = $derived(templates.data?.filter((t) => t.isRemote).length ?? 0);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.templates_local_templates(),
			value: localTemplatesCount,
			icon: FolderOpenIcon,
			iconColor: 'text-blue-500',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.templates_remote_templates(),
			value: remoteTemplatesCount,
			icon: GlobeIcon,
			iconColor: 'text-green-500',
			class: 'border-l-4 border-l-green-500'
		},
		{
			title: m.templates_registries(),
			value: registries.length,
			icon: LayersIcon,
			iconColor: 'text-purple-500',
			class: 'border-l-4 border-l-purple-500'
		}
	]);
</script>

<ResourcePageLayout
	title={m.templates_title()}
	subtitle={m.templates_subtitle()}
	{actionButtons}
	{statCards}
	statCardsColumns={3}
>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root bind:value={activeView}>
				<div class="pb-6">
					<div class="w-fit">
						<TabBar
							items={tabItems}
							value={activeView}
							onValueChange={(value) => (activeView = value as 'browse' | 'registries')}
						/>
					</div>
				</div>

				<Tabs.Content value="browse">
					<TemplatesBrowser bind:templates bind:requestOptions />
				</Tabs.Content>

				<Tabs.Content value="registries">
					<RegistryManager
						{registries}
						{isLoading}
						onAddRegistry={() => (showAddRegistrySheet = true)}
						onUpdateRegistry={updateRegistry}
						onRemoveRegistry={removeRegistry}
					/>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<AddTemplateRegistrySheet
			bind:open={showAddRegistrySheet}
			onSubmit={handleRegistrySubmit}
			isLoading={isLoading.addingRegistry}
		/>
	{/snippet}
</ResourcePageLayout>
