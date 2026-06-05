<script lang="ts">
	import SwarmKvPage from '$lib/components/swarm/swarm-kv-page.svelte';
	import { LockIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';

	let {}: PageProps = $props();

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canManageSecrets = $derived(hasPermission('swarm:secrets', currentEnvId));

	const messages = {
		pageTitle: m.swarm_secrets_title(),
		pageSubtitle: m.swarm_secrets_subtitle(),
		statTitle: m.swarm_secrets_title(),
		createTitle: m.swarm_secrets_create_title(),
		createSubtitle: m.swarm_secrets_create_subtitle(),
		namePlaceholder: m.swarm_secrets_name_placeholder(),
		dataPlaceholder: m.swarm_secrets_data_placeholder(),
		createButton: m.swarm_secrets_create_button(),
		listTitle: m.swarm_secrets_list_title(),
		listSubtitle: m.swarm_secrets_list_subtitle(),
		empty: m.swarm_secrets_empty(),
		immutableNotice: m.swarm_secrets_immutable_notice(),
		deleteButton: m.swarm_secrets_delete_button(),
		nameRequired: m.swarm_secrets_name_required(),
		createFailed: m.swarm_secrets_create_failed(),
		createSuccess: (name: string) => m.swarm_secrets_create_success({ name }),
		deleteConfirm: (name: string) => m.swarm_secrets_delete_confirm({ name }),
		deleteFailed: (name: string) => m.swarm_secrets_delete_failed({ name }),
		deleteSuccess: (name: string) => m.swarm_secrets_delete_success({ name })
	};
</script>

<SwarmKvPage
	icon={LockIcon}
	permission="swarm:secrets"
	resourceLabel={m.swarm_secret()}
	canManage={canManageSecrets}
	{messages}
	loadItems={() => swarmService.getSecrets()}
	createItem={(spec) => swarmService.createSecret({ spec })}
	removeItem={(id) => swarmService.removeSecret(id)}
/>
