<script lang="ts">
	import { SecretsIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import CreateSecretSheet from '$lib/components/sheets/create-secret-sheet.svelte';
	import type { SecretCreateRequest } from '$lib/types/secret.type';
	import SecretTable from './secret-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { secretService } from '$lib/services/secret-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let secrets = $state(untrack(() => data.secrets));
	let requestOptions = $state(untrack(() => data.secretRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	let isLoading = $state({ creating: false, refresh: false });

	const totalSecrets = $derived(
		secrets?.pagination?.grandTotalItems ?? secrets?.pagination?.totalItems ?? secrets?.data?.length ?? 0
	);

	async function refresh() {
		await parallelRefresh(
			{
				secrets: {
					fetch: () => secretService.getSecrets(requestOptions),
					onSuccess: (data) => {
						secrets = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.secrets_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handleCreate(options: SecretCreateRequest) {
		isLoading.creating = true;
		const name = options.name?.trim() || m.common_unknown();
		handleApiResultWithCallbacks({
			result: await tryCatch(secretService.createSecret(options)),
			message: m.common_create_failed({ resource: `${m.resource_secret()} "${name}"` }),
			setLoadingState: (v) => (isLoading.creating = v),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_secret()} "${name}"` }));
				secrets = await secretService.getSecrets(requestOptions);
				isCreateDialogOpen = false;
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_secret_cap() }),
			onclick: () => (isCreateDialogOpen = true),
			loading: isLoading.creating,
			disabled: isLoading.creating
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.secrets_stat_total(),
			value: totalSecrets,
			icon: SecretsIcon,
			iconColor: 'text-emerald-500'
		}
	]);
</script>

<ResourcePageLayout title={m.secrets_title()} subtitle={m.secrets_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SecretTable bind:secrets bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateSecretSheet bind:open={isCreateDialogOpen} isLoading={isLoading.creating} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
