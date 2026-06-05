<script lang="ts">
	import SwarmKvPage from '$lib/components/swarm/swarm-kv-page.svelte';
	import { TemplateIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';

	let {}: PageProps = $props();

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canManageConfigs = $derived(hasPermission('swarm:configs', currentEnvId));

	const messages = {
		pageTitle: m.swarm_configs_title(),
		pageSubtitle: m.swarm_configs_subtitle(),
		statTitle: m.swarm_configs_title(),
		createTitle: m.swarm_configs_create_title(),
		createSubtitle: m.swarm_configs_create_subtitle(),
		namePlaceholder: m.swarm_configs_name_placeholder(),
		dataPlaceholder: m.swarm_configs_data_placeholder(),
		createButton: m.swarm_configs_create_button(),
		listTitle: m.swarm_configs_list_title(),
		listSubtitle: m.swarm_configs_list_subtitle(),
		empty: m.swarm_configs_empty(),
		immutableNotice: m.swarm_configs_immutable_notice(),
		deleteButton: m.swarm_configs_delete_button(),
		nameRequired: m.swarm_configs_name_required(),
		createFailed: m.swarm_configs_create_failed(),
		createSuccess: (name: string) => m.swarm_configs_create_success({ name }),
		deleteConfirm: (name: string) => m.swarm_configs_delete_confirm({ name }),
		deleteFailed: (name: string) => m.swarm_configs_delete_failed({ name }),
		deleteSuccess: (name: string) => m.swarm_configs_delete_success({ name })
	};
</script>

<SwarmKvPage
	icon={TemplateIcon}
	permission="swarm:configs"
	resourceLabel={m.swarm_config()}
	canManage={canManageConfigs}
	{messages}
	loadItems={() => swarmService.getConfigs()}
	createItem={(spec) => swarmService.createConfig({ spec })}
	removeItem={(id) => swarmService.removeConfig(id)}
/>
