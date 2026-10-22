<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import PodmanSecretFormSheet from '#lib/components/sheets/podman-secret-form-sheet.svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { LockIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { PodmanSecret } from '#lib/types/podman.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { fetchAcrossEnvironments, type EnvironmentRow } from '#lib/utils/environments.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SecretsTable from './components/secrets-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const podmanEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses));
	const canCreate = $derived(podmanEnvironments.some((env) => hasPermission(PERMISSIONS.PODMAN_SECRETS, env.id)));
	let isSheetOpen = $state(false);
	let sheetSession = $state(0);
	let secretToEdit = $state<EnvironmentRow<PodmanSecret> | null>(null);

	const secretsQuery = createQuery(() => {
		const queryScope = pageState.scope;
		const environments = podmanEnvironments;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.podman.secrets(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getSecrets(id, request, config)),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});

	const resourcesReady = $derived(secretsQuery.data?.envId === pageState.scope);
	const secrets = $derived(secretsQuery.data?.value ?? data.secrets);

	async function loadSecrets(options: SearchPaginationSortRequest = pageState.requestOptions, requestedScope = pageState.scope) {
		const environments = podmanEnvironments;
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.podman.secrets(requestedScope, options),
			staleTime: 0,
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getSecrets(id, request, config))
		});
	}

	async function refresh() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(loadSecrets()),
			message: m.common_refresh_failed({ resource: m.podman_secrets_title() })
		});
	}

	function openSheet(secret: EnvironmentRow<PodmanSecret> | null) {
		secretToEdit = secret;
		sheetSession += 1;
		isSheetOpen = true;
	}

	async function openEditSheet(secret: EnvironmentRow<PodmanSecret>) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.getSecret(secret.spec.name, secret.environmentId)),
			message: m.common_load_failed({ resource: `${m.swarm_secret()} "${secret.spec.name}"` }),
			onSuccess: (latest) => openSheet({ ...secret, ...latest })
		});
	}

	useEnvironmentRefresh(() => {
		isSheetOpen = false;
	});

	async function handleSaved() {
		isSheetOpen = false;
		await refresh();
	}

	const isRefreshing = $derived(secretsQuery.isFetching && !secretsQuery.isPending);

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreate) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.create_secret(),
				onclick: () => openSheet(null)
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: secretsQuery.isFetching
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.podman_secrets_title(),
			value: resourcesReady ? (secrets.pagination?.totalItems ?? secrets.data.length) : 0,
			icon: LockIcon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout title={m.podman_secrets_title()} subtitle={m.podman_secrets_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			{#key pageState.scope}
				<SecretsTable
					bind:secrets={
						() => secrets,
						(value) => queryClient.setQueryData(queryKeys.podman.secrets(pageState.scope, pageState.requestOptions), value)
					}
					bind:requestOptions={pageState.requestOptions}
					onRefreshData={loadSecrets}
					onEdit={openEditSheet}
				/>
			{/key}
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		{#if sheetSession > 0}
			{#key sheetSession}
				<PodmanSecretFormSheet
					bind:open={isSheetOpen}
					environments={podmanEnvironments}
					existing={secretToEdit}
					onSaved={handleSaved}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>
