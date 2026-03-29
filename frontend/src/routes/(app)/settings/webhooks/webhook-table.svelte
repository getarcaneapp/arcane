<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Webhook } from '$lib/types/webhook.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
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
	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

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

	function actionTypeLabel(type: string): string {
		switch (type) {
			case 'update':
				return m.webhook_action_type_update();
			case 'start':
				return m.webhook_action_type_start();
			case 'stop':
				return m.webhook_action_type_stop();
			case 'restart':
				return m.webhook_action_type_restart();
			case 'redeploy':
				return m.webhook_action_type_redeploy();
			case 'up':
				return m.webhook_action_type_up();
			case 'down':
				return m.webhook_action_type_down();
			case 'run':
				return m.webhook_action_type_run();
			case 'sync':
				return m.webhook_action_type_sync();
			default:
				return type;
		}
	}

	function targetNameLabel(webhook: Webhook): string {
		return webhook.targetName?.trim() || '-';
	}

	function webhookStatusLabel(webhook: Webhook): string {
		return webhook.enabled ? m.webhook_status_enabled() : m.webhook_status_disabled();
	}

	function webhookStatusVariant(webhook: Webhook): 'green' | 'gray' {
		return webhook.enabled ? 'green' : 'gray';
	}

	function buildWebhookTableData(items: Webhook[]): Paginated<Webhook> {
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

	const tableData = $derived(buildWebhookTableData(webhooks));

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

	const columns = [
		{ accessorKey: 'name', title: m.webhook_col_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'enabled', title: m.webhook_col_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'tokenPrefix', title: m.webhook_col_token_prefix(), sortable: true, cell: TokenPrefixCell },
		{ accessorKey: 'targetType', title: m.webhook_col_target_type(), sortable: true, cell: TargetTypeCell },
		{ accessorKey: 'actionType', title: m.webhook_col_action_type(), sortable: true, cell: ActionTypeCell },
		{ accessorKey: 'targetName', title: m.webhook_col_target_name(), sortable: true, cell: TargetNameCell },
		{ accessorKey: 'lastTriggeredAt', title: m.webhook_col_last_triggered(), sortable: true, cell: LastTriggeredCell },
		{ accessorKey: 'createdAt', title: m.webhook_col_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<Webhook>[];

	const mobileFields = [
		{ id: 'enabled', label: m.webhook_col_status(), defaultVisible: true },
		{ id: 'tokenPrefix', label: m.webhook_col_token_prefix(), defaultVisible: true },
		{ id: 'targetType', label: m.webhook_col_target_type(), defaultVisible: true },
		{ id: 'actionType', label: m.webhook_col_action_type(), defaultVisible: true },
		{ id: 'targetName', label: m.webhook_col_target_name(), defaultVisible: true },
		{ id: 'lastTriggeredAt', label: m.webhook_col_last_triggered(), defaultVisible: true },
		{ id: 'createdAt', label: m.webhook_col_created(), defaultVisible: false }
	];
</script>

{#if webhooks.length === 0}
	<div class="text-muted-foreground flex flex-col items-center justify-center py-12 text-sm">
		<GlobeIcon class="mb-3 size-10 opacity-40" />
		<p>{m.webhook_empty_title()}</p>
		<p class="mt-1">{m.webhook_empty_description()}</p>
	</div>
{:else}
	<ArcaneTable
		persistKey="arcane-webhooks-table"
		items={tableData}
		bind:requestOptions
		bind:mobileFieldVisibility
		selectionDisabled={true}
		withoutSearch
		withoutPagination
		onRefresh={async () => {
			await onWebhooksChanged();
			return buildWebhookTableData(webhooks);
		}}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={WebhookMobileCardSnippet}
	/>
{/if}

{#snippet NameCell({ item }: { item: Webhook })}
	<span class="font-medium">{item.name}</span>
{/snippet}

{#snippet StatusCell({ item }: { item: Webhook })}
	<StatusBadge text={webhookStatusLabel(item)} variant={webhookStatusVariant(item)} />
{/snippet}

{#snippet TokenPrefixCell({ item }: { item: Webhook })}
	<div class="flex items-center gap-2">
		<code class="bg-muted rounded px-2 py-1 text-xs">{item.tokenPrefix}...</code>
		<CopyButton text={item.tokenPrefix} class="size-6" />
	</div>
{/snippet}

{#snippet TargetTypeCell({ item }: { item: Webhook })}
	<span class="bg-muted rounded px-2 py-1 text-xs font-medium">{targetTypeLabel(item.targetType)}</span>
{/snippet}

{#snippet ActionTypeCell({ item }: { item: Webhook })}
	<span class="bg-muted rounded px-2 py-1 text-xs font-medium">{actionTypeLabel(item.actionType)}</span>
{/snippet}

{#snippet TargetNameCell({ item }: { item: Webhook })}
	<span class="text-muted-foreground">{targetNameLabel(item)}</span>
{/snippet}

{#snippet LastTriggeredCell({ item }: { item: Webhook })}
	<span class="text-muted-foreground">{formatDate(item.lastTriggeredAt)}</span>
{/snippet}

{#snippet CreatedCell({ item }: { item: Webhook })}
	<span class="text-muted-foreground">{formatDate(item.createdAt)}</span>
{/snippet}

{#snippet WebhookMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: Webhook;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: GlobeIcon, variant: 'blue' }}
		title={(item: Webhook) => item.name}
		subtitle={(item: Webhook) => ((mobileFieldVisibility['targetName'] ?? true) ? targetNameLabel(item) : null)}
		badges={[
			(item: Webhook) => ({
				variant: webhookStatusVariant(item),
				text: webhookStatusLabel(item)
			})
		]}
		fields={[
			{
				label: m.webhook_col_token_prefix(),
				getValue: (item: Webhook) => `${item.tokenPrefix}...`,
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['tokenPrefix'] ?? true
			},
			{
				label: m.webhook_col_target_type(),
				getValue: (item: Webhook) => targetTypeLabel(item.targetType),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['targetType'] ?? true
			},
			{
				label: m.webhook_col_action_type(),
				getValue: (item: Webhook) => actionTypeLabel(item.actionType),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['actionType'] ?? true
			},
			{
				label: m.webhook_col_target_name(),
				getValue: (item: Webhook) => targetNameLabel(item),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['targetName'] ?? true
			},
			{
				label: m.webhook_col_last_triggered(),
				getValue: (item: Webhook) => formatDate(item.lastTriggeredAt),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastTriggeredAt'] ?? true
			},
			{
				label: m.webhook_col_created(),
				getValue: (item: Webhook) => formatDate(item.createdAt),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['createdAt'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Webhook })}
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
				<DropdownMenu.Item onclick={() => handleToggleWebhook(item)} disabled={isLoading.toggling}>
					{item.enabled ? m.webhook_disable() : m.webhook_enable()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteWebhook(item.id, item.name)}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}
