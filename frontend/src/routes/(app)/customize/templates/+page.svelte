<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import AddTemplateRegistrySheet from '#lib/components/sheets/add-template-registry-sheet.svelte';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { RegistryIcon, TemplateIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { templateListQuery, templateRegistriesQuery } from '#lib/query/templates.js';
	import { templateService } from '#lib/services/template-service.js';
	import type { TemplateRegistryCreate, TemplateRegistryUpdate } from '#lib/types/swarm.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasShownPermission } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RegistryManager from './components/registry-manager.svelte';
	import TemplateGallery from './components/template-gallery.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.templateRequestOptions));
	const templatesQuery = createQuery(() => ({
		...templateListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const templates = $derived(templatesQuery.data ?? data.templates);
	const registriesQuery = createQuery(() => templateRegistriesQuery());
	const registries = $derived(registriesQuery.data ?? data.registries);
	const tabItems: TabItem[] = [
		{
			value: 'browse',
			label: m.templates_browse_templates(),
			icon: TemplateIcon
		},
		{
			value: 'registries',
			label: m.templates_manage_registries(),
			icon: RegistryIcon
		}
	];
	const urlTab = useUrlTab({
		validTabs: () => tabItems.map((tab) => tab.value),
		defaultTab: () => 'browse'
	});
	const activeView = $derived(urlTab.value);

	const isLoading = $state({
		removing: {} as Record<string, boolean>,
		updating: {} as Record<string, boolean>
	});

	let showAddRegistrySheet = $state(false);

	async function invalidateTemplateData() {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: queryKeys.templates.all }),
			queryClient.invalidateQueries({ queryKey: queryKeys.templates.registries() })
		]);
	}

	const updateRegistryMutation = createMutation(() => ({
		mutationFn: ({ id, updates }: { id: string; updates: TemplateRegistryUpdate }) => templateService.updateRegistry(id, updates),
		onMutate: ({ id }) => {
			isLoading.updating[id] = true;
		},
		onSuccess: async () => {
			await invalidateTemplateData();
			toast.success(m.common_update_success({ resource: m.resource_registry() }));
		},
		onError: (error) => {
			toast.error(m.registries_save_failed(), { description: extractApiErrorMessage(error) });
		},
		onSettled: (_result, _error, { id }) => {
			delete isLoading.updating[id];
		}
	}));

	const removeRegistryMutation = createMutation(() => ({
		mutationFn: (id: string) => templateService.deleteRegistry(id),
		onMutate: (id) => {
			isLoading.removing[id] = true;
		},
		onSuccess: async (_result, id) => {
			const reg = registries.find((r) => r.id === id);
			await invalidateTemplateData();
			toast.success(
				reg
					? m.common_delete_success({ resource: `${m.resource_registry()} "${reg.url}"` })
					: m.templates_registry_removed_success()
			);
		},
		onError: (error) => {
			toast.error(m.registries_save_failed(), { description: extractApiErrorMessage(error) });
		},
		onSettled: (_result, _error, id) => {
			delete isLoading.removing[id];
		}
	}));

	const addRegistryMutation = createMutation(() => ({
		mutationFn: (registry: TemplateRegistryCreate) =>
			templateService.addRegistry({
				name: registry.name.trim(),
				url: registry.url.trim(),
				description: registry.description?.trim() || undefined,
				enabled: registry.enabled
			}),
		onSuccess: async () => {
			await invalidateTemplateData();
			showAddRegistrySheet = false;
			toast.success(m.common_create_success({ resource: m.resource_registry() }));
		},
		onError: (error) => {
			toast.error(m.registries_save_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	function updateRegistry(id: string, updates: TemplateRegistryUpdate) {
		if (isLoading.updating[id]) return;
		if (!registries.some((r) => r.id === id)) {
			toast.error(m.templates_registry_not_found());
			return;
		}
		updateRegistryMutation.mutate({ id, updates });
	}

	function removeRegistry(id: string) {
		if (isLoading.removing[id]) return;
		removeRegistryMutation.mutate(id);
	}

	async function refreshTemplates() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(templatesQuery.refetch({ throwOnError: true })),
			message: m.common_refresh_failed({ resource: m.templates_title() }),
			setLoadingState: () => {},
			onSuccess: () => {
				toast.success(m.templates_refreshed());
			}
		});
	}

	const canCreateTemplate = $derived(hasShownPermission('templates:create'));
	const canUpdateTemplate = $derived(hasShownPermission('templates:update'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateTemplate) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.templates_create_template(),
				onclick: () => goto(resolve('/(app)/customize/templates/create'))
			});
		}
		if (canUpdateTemplate) {
			buttons.push({
				id: 'default',
				action: 'edit',
				label: m.templates_edit_default(),
				onclick: () => goto(resolve('/(app)/customize/templates/default'))
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refreshTemplates
		});
		return buttons;
	});

	const localTemplatesCount = $derived(templates.data?.filter((t) => !t.isRemote).length ?? 0);
	const remoteTemplatesCount = $derived(templates.data?.filter((t) => t.isRemote).length ?? 0);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.templates_local_templates(),
			value: localTemplatesCount,
			icon: FolderOpenIcon,
			iconColor: 'text-info'
		},
		{
			title: m.templates_remote_templates(),
			value: remoteTemplatesCount,
			icon: RegistryIcon,
			iconColor: 'text-success'
		},
		{
			title: m.templates_registries(),
			value: registries.length,
			icon: TemplateIcon,
			iconColor: 'text-purple'
		}
	]);
</script>

<ResourcePageLayout title={m.templates_title()} subtitle={m.templates_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root value={activeView}>
				<div class="pb-6">
					<div class="w-fit">
						<TabBar items={tabItems} value={activeView} onValueChange={urlTab.select} />
					</div>
				</div>

				<Tabs.Content value="browse">
					<TemplateGallery {templates} bind:requestOptions={pageState.requestOptions} />
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
			onSubmit={(registry) => addRegistryMutation.mutate(registry)}
			isLoading={addRegistryMutation.isPending}
		/>
	{/snippet}
</ResourcePageLayout>
