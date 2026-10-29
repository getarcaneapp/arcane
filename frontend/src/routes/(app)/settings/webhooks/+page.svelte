<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import WebhookFormSheet from '#lib/components/sheets/webhook-form-sheet.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Snippet } from '#lib/components/ui/snippet/index.js';
	import { GlobeIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { webhookService } from '#lib/services/webhook-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Webhook, WebhookCreated, WebhookCreate } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { withBase } from '#lib/utils/base-path.js';
	import { type EnvironmentPage, fetchAcrossEnvironments, readableEnvironments } from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import WebhookTable from './components/webhook-table.svelte';

	const queryClient = useQueryClient();
	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });

	function webhookPage(items: Webhook[]): Paginated<Webhook> {
		return {
			data: items,
			pagination: {
				totalPages: 1,
				totalItems: items.length,
				currentPage: 1,
				itemsPerPage: Math.max(items.length, 1)
			}
		};
	}

	const webhooksQuery = createQuery(() => {
		const environments = readableEnvironments('webhooks:list');
		const options = requestOptions;
		return {
			queryKey: queryKeys.webhooks.list(environmentStore.shownKey, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (scope, _request, config) =>
					webhookService.getWebhooks(scope, config).then(webhookPage)
				)
		};
	});

	const webhooks = $derived<EnvironmentPage<Paginated<Webhook>>>(
		webhooksQuery.data ?? { data: [], pagination: webhookPage([]).pagination }
	);

	async function loadWebhooks(options: SearchPaginationSortRequest = requestOptions) {
		requestOptions = options;
		const result = await webhooksQuery.refetch();
		return result.data ?? webhooks;
	}

	async function refreshWebhooks() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.webhooks.all });
	}

	let isDialogOpen = $state({
		create: false,
		showToken: false
	});

	let newlyCreatedWebhook = $state<WebhookCreated | null>(null);

	let isLoading = $state({ creating: false });

	let newlyCreatedWebhookUrl = $derived.by(() => {
		if (!newlyCreatedWebhook?.token) {
			return '';
		}

		const origin = globalThis.location?.origin ?? '';
		return `${origin}${withBase('/api')}/webhooks/trigger/${encodeURIComponent(newlyCreatedWebhook.token)}`;
	});

	async function handleCreateWebhook(webhook: WebhookCreate, environmentId: string) {
		isLoading.creating = true;
		const result = await tryCatch(webhookService.create(webhook, environmentId));
		await handleApiResultWithCallbacks({
			result,
			message: m.webhook_create_failed({ name: webhook.name }),
			setLoadingState: (value) => (isLoading.creating = value),
			onSuccess: async (created) => {
				toast.success(m.webhook_create_success({ name: webhook.name }));
				await refreshWebhooks();
				isDialogOpen.create = false;
				newlyCreatedWebhook = created as WebhookCreated;
				isDialogOpen.showToken = true;
			}
		});
	}

	const actionButtons: ActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			placement: 'primary',
			label: m.create_webhook(),
			onclick: () => (isDialogOpen.create = true),
			loading: isLoading.creating,
			disabled: isLoading.creating
		}
	]);
</script>

<SettingsPageLayout
	title={m.webhook_page_title()}
	description={m.webhook_page_description()}
	icon={GlobeIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<WebhookTable {webhooks} bind:requestOptions onRefresh={loadWebhooks} onWebhooksChanged={refreshWebhooks} />
	{/snippet}

	{#snippet additionalContent()}
		<WebhookFormSheet bind:open={isDialogOpen.create} onSubmit={handleCreateWebhook} isLoading={isLoading.creating} />

		<ResponsiveDialog.Root
			bind:open={isDialogOpen.showToken}
			title={m.webhook_created_title()}
			description={m.webhook_created_description()}
			contentClass="!max-w-fit"
		>
			{#snippet children()}
				<div class="space-y-4 py-4">
					<div class="rounded-lg bg-muted p-4">
						<p class="mb-2 text-sm font-medium text-muted-foreground">{m.webhook_url()}</p>
						<Snippet
							text={newlyCreatedWebhookUrl}
							onCopy={(status) => {
								if (status === 'success') {
									toast.success(m.webhook_token_copied());
								}
							}}
						/>
					</div>
					<div class="rounded-lg border border-warning/30 bg-warning/10 p-4">
						<p class="text-sm text-warning">
							<strong>{m.common_important()}:</strong>
							{m.webhook_token_warning()}
						</p>
					</div>
				</div>
			{/snippet}
			{#snippet footer()}
				<ArcaneButton action="confirm" onclick={() => (isDialogOpen.showToken = false)} customLabel={m.common_done()} />
			{/snippet}
		</ResponsiveDialog.Root>
	{/snippet}
</SettingsPageLayout>
