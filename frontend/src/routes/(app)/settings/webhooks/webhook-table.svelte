<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Webhook } from '$lib/types/webhook.type';
	import { webhookService } from '$lib/services/webhook-service';
	import { TrashIcon, EllipsisIcon, GlobeIcon } from '$lib/icons';
	import * as m from '$lib/paraglide/messages.js';

	let {
		webhooks = $bindable(),
		onWebhooksChanged
	}: {
		webhooks: Webhook[];
		onWebhooksChanged: () => Promise<void>;
	} = $props();

	let isLoading = $state({ removing: false, toggling: false });

	function formatDate(dateString?: string): string {
		if (!dateString) return '-';
		return new Date(dateString).toLocaleString();
	}

	function targetTypeLabel(type: string): string {
		switch (type) {
			case 'container':
				return m.webhook_type_container();
			case 'project':
				return m.webhook_type_project();
			case 'updater':
				return m.webhook_type_updater();
			case 'gitops':
				return m.webhook_type_gitops();
			default:
				return type;
		}
	}

	async function handleToggleWebhook(webhook: Webhook) {
		const name = webhook.name;
		const enabling = !webhook.enabled;
		isLoading.toggling = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(webhookService.update(webhook.id, { enabled: enabling })),
			message: enabling ? m.webhook_enable_failed({ name }) : m.webhook_disable_failed({ name }),
			setLoadingState: (value) => (isLoading.toggling = value),
			onSuccess: async () => {
				toast.success(enabling ? m.webhook_enable_success({ name }) : m.webhook_disable_success({ name }));
				await onWebhooksChanged();
			}
		});
	}

	async function handleDeleteWebhook(webhookId: string, name: string) {
		openConfirmDialog({
			title: m.webhook_delete_title({ name }),
			message: m.webhook_delete_message({ name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(webhookService.delete(webhookId)),
						message: m.webhook_delete_failed({ name }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.webhook_delete_success({ name }));
							await onWebhooksChanged();
						}
					});
				}
			}
		});
	}
</script>

{#if webhooks.length === 0}
	<div class="text-muted-foreground flex flex-col items-center justify-center py-12 text-sm">
		<GlobeIcon class="mb-3 size-10 opacity-40" />
		<p>{m.webhook_empty_title()}</p>
		<p class="mt-1">{m.webhook_empty_description()}</p>
	</div>
{:else}
	<div class="rounded-md border">
		<table class="w-full text-sm">
			<thead>
				<tr class="border-b">
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_name()}</th>
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_status()}</th>
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_token_prefix()}</th>
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_target_type()}</th>
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_last_triggered()}</th>
					<th class="text-muted-foreground px-4 py-3 text-left font-medium">{m.webhook_col_created()}</th>
					<th class="w-10 px-4 py-3"></th>
				</tr>
			</thead>
			<tbody>
				{#each webhooks as webhook (webhook.id)}
					<tr class="hover:bg-muted/50 border-b last:border-b-0">
						<td class="px-4 py-3 font-medium">{webhook.name}</td>
						<td class="px-4 py-3">
							<span
								class="rounded px-2 py-1 text-xs font-medium {webhook.enabled
									? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
									: 'bg-muted text-muted-foreground'}"
							>
								{webhook.enabled ? m.webhook_status_enabled() : m.webhook_status_disabled()}
							</span>
						</td>
						<td class="px-4 py-3">
							<div class="flex items-center gap-2">
								<code class="bg-muted rounded px-2 py-1 text-xs">{webhook.tokenPrefix}...</code>
								<CopyButton text={webhook.tokenPrefix} class="size-6" />
							</div>
						</td>
						<td class="px-4 py-3">
							<span class="bg-muted rounded px-2 py-1 text-xs font-medium">{targetTypeLabel(webhook.targetType)}</span>
						</td>
						<td class="text-muted-foreground px-4 py-3">{formatDate(webhook.lastTriggeredAt)}</td>
						<td class="text-muted-foreground px-4 py-3">{formatDate(webhook.createdAt)}</td>
						<td class="px-4 py-3">
							<DropdownMenu.Root>
								<DropdownMenu.Trigger>
									{#snippet child({ props })}
										<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
											<span class="sr-only">{m.common_open_menu()}</span>
											<EllipsisIcon class="size-4" />
										</ArcaneButton>
									{/snippet}
								</DropdownMenu.Trigger>
								<DropdownMenu.Content align="end">
									<DropdownMenu.Group>
										<DropdownMenu.Item onclick={() => handleToggleWebhook(webhook)} disabled={isLoading.toggling}>
											{webhook.enabled ? m.webhook_disable() : m.webhook_enable()}
										</DropdownMenu.Item>
										<DropdownMenu.Separator />
										<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteWebhook(webhook.id, webhook.name)}>
											<TrashIcon class="size-4" />
											{m.common_delete()}
										</DropdownMenu.Item>
									</DropdownMenu.Group>
								</DropdownMenu.Content>
							</DropdownMenu.Root>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
