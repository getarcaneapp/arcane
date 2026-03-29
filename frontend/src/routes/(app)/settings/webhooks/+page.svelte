<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import WebhookTable from './webhook-table.svelte';
	import WebhookFormSheet from '$lib/components/sheets/webhook-form-sheet.svelte';
	import type { Webhook, WebhookCreated, CreateWebhook } from '$lib/types/webhook.type';
	import { webhookService } from '$lib/services/webhook-service';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts/index.js';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Snippet } from '$lib/components/ui/snippet/index.js';
	import { untrack } from 'svelte';
	import { GlobeIcon } from '$lib/icons';
	import * as m from '$lib/paraglide/messages.js';

	let { data } = $props();

	let webhooks = $state<Webhook[]>(untrack(() => data.webhooks));

	let isDialogOpen = $state({
		create: false,
		showToken: false
	});

	let newlyCreatedWebhook = $state<WebhookCreated | null>(null);

	let isLoading = $state({ creating: false });

	async function handleCreateWebhook(webhook: CreateWebhook) {
		isLoading.creating = true;
		const result = await tryCatch(webhookService.create(webhook));
		handleApiResultWithCallbacks({
			result,
			message: m.webhook_create_failed({ name: webhook.name }),
			setLoadingState: (value) => (isLoading.creating = value),
			onSuccess: async (created) => {
				toast.success(m.webhook_create_success({ name: webhook.name }));
				webhooks = await webhookService.getWebhooks();
				isDialogOpen.create = false;
				newlyCreatedWebhook = created as WebhookCreated;
				isDialogOpen.showToken = true;
			}
		});
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			label: m.webhook_create_button(),
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
		<WebhookTable
			bind:webhooks
			onWebhooksChanged={async () => {
				webhooks = await webhookService.getWebhooks();
			}}
		/>
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
					<div class="bg-muted rounded-lg p-4">
						<p class="text-muted-foreground mb-2 text-sm font-medium">{m.webhook_token_label()}</p>
						<Snippet
							text={newlyCreatedWebhook?.token || ''}
							onCopy={(status) => {
								if (status === 'success') {
									toast.success(m.webhook_token_copied());
								}
							}}
						/>
					</div>
					<div class="rounded-lg border border-yellow-200 bg-yellow-50 p-4 dark:border-yellow-800 dark:bg-yellow-900/20">
						<p class="text-sm text-yellow-800 dark:text-yellow-200">
							<strong>{m.common_important()}:</strong>
							{m.webhook_token_warning()}
							<code class="rounded bg-yellow-100 px-1 dark:bg-yellow-900/40">/api/webhooks/trigger/&lt;token&gt;</code>.
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
