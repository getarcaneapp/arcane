<script lang="ts">
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { tick } from 'svelte';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { DownloadIcon, ShieldCheckIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { auditService } from '#lib/services/audit-service.js';
	import type { AuditExportFormat, AuditOutcome } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { downloadBlob } from '#lib/utils/browser-download.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { auditOutcomeLabel } from './components/audit-labels.js';
	import AuditTable from './components/audit-table.svelte';

	let { data } = $props();

	const ALL_OUTCOMES = 'all';
	const outcomes: AuditOutcome[] = ['success', 'denied', 'error'];

	let requestOptions = $derived(data.auditRequestOptions);
	let selectedIds = $state<string[]>([]);
	let exporting = $state<AuditExportFormat | null>(null);

	const logsQuery = createQuery(() => {
		const options = requestOptions;
		return {
			queryKey: queryKeys.audit.list(options),
			queryFn: () => auditService.getLogs(options),
			placeholderData: keepPreviousData
		};
	});

	const logs = $derived(logsQuery.data ?? data.logs);
	const isRefreshing = $derived(logsQuery.isFetching && !logsQuery.isPending);
	const outcomeFilter = $derived(String(requestOptions.filters?.['outcome'] ?? ALL_OUTCOMES));

	function setOutcome(value: string) {
		const filters = { ...requestOptions.filters };
		if (value === ALL_OUTCOMES) {
			delete filters['outcome'];
		} else {
			filters['outcome'] = value;
		}
		requestOptions = {
			...requestOptions,
			filters: Object.keys(filters).length ? filters : undefined,
			pagination: { page: 1, limit: requestOptions.pagination?.limit ?? 20 }
		};
	}

	async function exportLogs(format: AuditExportFormat) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(auditService.export(format, requestOptions.filters)),
			message: m.audit_export_failed(),
			setLoadingState: (value) => (exporting = value ? format : null),
			onSuccess: ({ blob, filename }) => downloadBlob(blob, filename)
		});
	}

	const actionButtons: ActionButton[] = $derived([
		...(hasPermission(PERMISSIONS.AUDIT_EXPORT)
			? [
					{
						id: 'export-csv',
						action: 'base' as const,
						placement: 'primary' as const,
						icon: DownloadIcon,
						label: m.audit_export_csv(),
						onclick: () => void exportLogs('csv'),
						loading: exporting === 'csv',
						disabled: exporting !== null
					},
					{
						id: 'export-ndjson',
						action: 'base' as const,
						icon: DownloadIcon,
						label: m.audit_export_ndjson(),
						onclick: () => void exportLogs('ndjson'),
						loading: exporting === 'ndjson',
						disabled: exporting !== null
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'refresh' as const,
			placement: 'secondary' as const,
			iconOnly: true,
			label: m.common_refresh(),
			onclick: () => void logsQuery.refetch(),
			loading: isRefreshing,
			disabled: isRefreshing
		}
	]);
</script>

<SettingsPageLayout
	title={m.audit_title()}
	description={m.audit_subtitle()}
	icon={ShieldCheckIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<div class="space-y-4">
			<div class="flex justify-end">
				<Select.Root type="single" value={outcomeFilter} onValueChange={setOutcome}>
					<Select.Trigger class="w-48" aria-label={m.audit_col_outcome()}>
						<span>{outcomeFilter === ALL_OUTCOMES ? m.audit_outcome_all() : auditOutcomeLabel(outcomeFilter)}</span>
					</Select.Trigger>
					<Select.Content>
						<Select.Item value={ALL_OUTCOMES} label={m.audit_outcome_all()}>{m.audit_outcome_all()}</Select.Item>
						{#each outcomes as outcome (outcome)}
							<Select.Item value={outcome} label={auditOutcomeLabel(outcome)}>{auditOutcomeLabel(outcome)}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
			<AuditTable
				{logs}
				bind:selectedIds
				bind:requestOptions
				onRefreshData={async (options) => {
					requestOptions = options;
					await tick();
					await logsQuery.refetch({ cancelRefetch: false });
				}}
			/>
		</div>
	{/snippet}
</SettingsPageLayout>
