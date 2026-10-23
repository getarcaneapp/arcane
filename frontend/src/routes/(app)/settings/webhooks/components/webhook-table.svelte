<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { TrashIcon, GlobeIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { webhookService } from '#lib/services/webhook-service.js';
	import type { Webhook } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import type { EnvironmentPage, EnvironmentRow } from '#lib/utils/environments.js';
	import { formatDateTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type WebhookRow = EnvironmentRow<Webhook>;

	let {
		webhooks,
		requestOptions = $bindable(),
		onRefresh,
		onWebhooksChanged
	}: {
		webhooks: EnvironmentPage<Paginated<Webhook>>;
		requestOptions: SearchPaginationSortRequest;
		onRefresh: (options: SearchPaginationSortRequest) => Promise<EnvironmentPage<Paginated<Webhook>>>;
		onWebhooksChanged: () => Promise<void>;
	} = $props();

	let isLoading = $state({ removing: false, toggling: false });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	function formatDate(dateString?: string): string {
		if (!dateString) return '-';
		return formatDateTime(dateString) || dateString;
	}

	function targetTypeLabel(type: string): string {
		switch (type) {
			case 'container':
				return m.container();
			case 'project':
				return m.project();
			case 'updater':
				return m.updater();
			case 'gitops':
				return m.gitops();
			default:
				return type;
		}
	}

	function actionTypeLabel(type: string): string {
		switch (type) {
			case 'update':
				return m.common_update();
			case 'start':
				return m.common_start();
			case 'stop':
				return m.common_stop();
			case 'restart':
				return m.common_restart();
			case 'redeploy':
				return m.common_redeploy();
			case 'up':
				return m.common_up();
			case 'down':
				return m.common_down();
			case 'run':
				return m.webhook_action_type_run();
			case 'sync':
				return m.resource_sync_cap();
			default:
				return type;
		}
	}

	function targetNameLabel(webhook: Webhook): string {
		return webhook.targetName?.trim() || '-';
	}

	function webhookStatusLabel(webhook: Webhook): string {
		return webhook.enabled ? m.common_enabled() : m.common_disabled();
	}

	function webhookStatusVariant(webhook: Webhook): 'green' | 'gray' {
		return webhook.enabled ? 'green' : 'gray';
	}

	async function handleToggleWebhook(webhook: WebhookRow) {
		const name = webhook.name;
		const enabling = !webhook.enabled;
		isLoading.toggling = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(webhookService.update(webhook.id, { enabled: enabling }, webhook.environmentId)),
			message: enabling ? m.webhook_enable_failed({ name }) : m.webhook_disable_failed({ name }),
			setLoadingState: (value) => (isLoading.toggling = value),
			onSuccess: async () => {
				toast.success(enabling ? m.webhook_enable_success({ name }) : m.webhook_disable_success({ name }));
				await onWebhooksChanged();
			}
		});
	}

	async function handleDeleteWebhook(webhook: WebhookRow) {
		const name = webhook.name;
		openConfirmDialog({
			title: m.webhook_delete_title({ name }),
			message: m.webhook_delete_message({ name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(webhookService.delete(webhook.id, webhook.environmentId)),
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
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		...environmentColumns<WebhookRow>(),
		{ accessorKey: 'enabled', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'tokenPrefix', title: m.webhook_col_token_prefix(), sortable: true, cell: TokenPrefixCell },
		{ accessorKey: 'targetType', title: m.target_type(), sortable: true, cell: TargetTypeCell },
		{ accessorKey: 'actionType', title: m.webhook_col_action_type(), sortable: true, cell: ActionTypeCell },
		{ accessorKey: 'targetName', title: m.target(), sortable: true, cell: TargetNameCell },
		{ accessorKey: 'lastTriggeredAt', title: m.webhook_col_last_triggered(), sortable: true, cell: LastTriggeredCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<WebhookRow>[];

	const mobileFields = [
		{ id: 'enabled', label: m.common_status(), defaultVisible: true },
		{ id: 'tokenPrefix', label: m.webhook_col_token_prefix(), defaultVisible: true },
		{ id: 'targetType', label: m.target_type(), defaultVisible: true },
		{ id: 'actionType', label: m.webhook_col_action_type(), defaultVisible: true },
		{ id: 'targetName', label: m.target(), defaultVisible: true },
		{ id: 'lastTriggeredAt', label: m.webhook_col_last_triggered(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: false }
	];
</script>

<ArcaneTable
	persistKey="arcane-webhooks-table"
	items={webhooks}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	withoutSearch
	withoutPagination
	{onRefresh}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={WebhookMobileCardSnippet}
>
	{#snippet emptyState()}
		<EmptyState variant="plain" icon={GlobeIcon} title={m.webhook_empty_title()} description={m.webhook_empty_description()} />
	{/snippet}
</ArcaneTable>

{#snippet NameCell({ item }: { item: WebhookRow })}
	<span class="font-medium">{item.name}</span>
{/snippet}

{#snippet StatusCell({ item }: { item: WebhookRow })}
	<Badge variant={webhookStatusVariant(item)} minWidth="20">{webhookStatusLabel(item)}</Badge>
{/snippet}

{#snippet TokenPrefixCell({ item }: { item: WebhookRow })}
	<div class="flex items-center gap-2">
		<code class="rounded bg-muted px-2 py-1 text-xs">{item.tokenPrefix}...</code>
		<CopyButton text={item.tokenPrefix} class="size-6" />
	</div>
{/snippet}

{#snippet TargetTypeCell({ item }: { item: WebhookRow })}
	<span class="rounded bg-muted px-2 py-1 text-xs font-medium">{targetTypeLabel(item.targetType)}</span>
{/snippet}

{#snippet ActionTypeCell({ item }: { item: WebhookRow })}
	<span class="rounded bg-muted px-2 py-1 text-xs font-medium">{actionTypeLabel(item.actionType)}</span>
{/snippet}

{#snippet TargetNameCell({ item }: { item: WebhookRow })}
	<span class="text-muted-foreground">{targetNameLabel(item)}</span>
{/snippet}

{#snippet LastTriggeredCell({ item }: { item: WebhookRow })}
	<span class="text-muted-foreground">{formatDate(item.lastTriggeredAt)}</span>
{/snippet}

{#snippet CreatedCell({ item }: { item: WebhookRow })}
	<span class="text-muted-foreground">{formatDate(item.createdAt)}</span>
{/snippet}

{#snippet WebhookMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: WebhookRow;
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
				label: m.target_type(),
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
				label: m.target(),
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
				label: m.common_created(),
				getValue: (item: Webhook) => formatDate(item.createdAt),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['createdAt'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: WebhookRow })}
	<RowActionsMenu>
		<IfPermitted perm="webhooks:update" envId={item.environmentId}>
			<DropdownMenu.Item onclick={() => handleToggleWebhook(item)} disabled={isLoading.toggling}>
				{item.enabled ? m.common_disable() : m.common_enable()}
			</DropdownMenu.Item>
		</IfPermitted>
		<DropdownMenu.Separator />
		<IfPermitted perm="webhooks:delete" envId={item.environmentId}>
			<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteWebhook(item)}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
