<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { BitwardenScope, BitwardenTarget } from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	let {
		sourceId,
		target = $bindable()
	}: {
		sourceId: string;
		target: BitwardenTarget;
	} = $props();

	const scopes: { value: BitwardenScope; label: () => string; description: () => string }[] = [
		{ value: 'folder', label: m.bitwarden_scope_folder, description: m.bitwarden_scope_folder_description },
		{ value: 'collection', label: m.bitwarden_scope_collection, description: m.bitwarden_scope_collection_description },
		{ value: 'item', label: m.bitwarden_scope_item, description: m.bitwarden_scope_item_description }
	];

	const browseKind = $derived(`${target.scope}s`);
	const optionsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, browseKind),
		queryFn: () => secretSourceService.browse(sourceId, { kind: browseKind }),
		enabled: !!sourceId,
		retry: false
	}));
	const options = $derived(optionsQuery.data ?? []);
	const selected = $derived(options.find((option) => option.id === target.id));

	function pickerLabel(scope: BitwardenScope): string {
		switch (scope) {
			case 'collection':
				return m.bitwarden_select_collection();
			case 'item':
				return m.bitwarden_select_item();
			default:
				return m.bitwarden_select_folder();
		}
	}
</script>

<div class="space-y-2">
	<Label class="mb-0">{m.bitwarden_scope()}</Label>
	<RadioGroup.Root
		class="mt-2"
		value={target.scope}
		onValueChange={(value) => {
			target = { scope: value as BitwardenScope, id: '', name: '' };
		}}
	>
		<div class="grid gap-2">
			{#each scopes as scope (scope.value)}
				<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
					<RadioGroup.Item value={scope.value} class="mt-0.5" />
					<div class="grid gap-1 leading-none">
						<span class="text-sm font-medium">{scope.label()}</span>
						<span class="text-xs text-muted-foreground">{scope.description()}</span>
					</div>
				</label>
			{/each}
		</div>
	</RadioGroup.Root>
</div>

<div class="space-y-2">
	<Label for="bitwarden-target">{pickerLabel(target.scope)}</Label>
	{#if optionsQuery.isPending}
		<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
	{:else if optionsQuery.isError}
		<Alert.Root
			variant="destructive-subtle"
			icon={AlertIcon}
			heading={m.secret_sources_test_failed()}
			description={extractApiErrorMessage(optionsQuery.error)}
		/>
	{:else}
		<Select.Root
			type="single"
			value={target.id}
			onValueChange={(value) => {
				const option = options.find((candidate) => candidate.id === value);
				target = { ...target, id: value, name: option?.name ?? '' };
			}}
		>
			<Select.Trigger id="bitwarden-target" class="w-full">
				<span>{selected?.name ?? target.name ?? pickerLabel(target.scope)}</span>
			</Select.Trigger>
			<Select.Content>
				{#each options as option (option.id)}
					<Select.Item value={option.id}>
						<div class="flex flex-col">
							<span>{option.name}</span>
							{#if option.detail}
								<span class="text-xs text-muted-foreground">{option.detail}</span>
							{/if}
						</div>
					</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	{/if}
</div>
