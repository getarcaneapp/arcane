<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { SvelteSet } from 'svelte/reactivity';

	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { ComposeRefsResult } from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ComposeRefsPicker from './compose-refs-picker.svelte';
	import { selectionToAssignments, selectionToEnvKeys } from './compose-refs.js';

	let {
		open = $bindable(false),
		compose,
		keys,
		env = null,
		composeLocked = false,
		onApply
	}: {
		open: boolean;
		compose: string;
		keys: string[];
		// The project's .env content; null leaves .env out.
		env?: string | null;
		composeLocked?: boolean;
		// Receives the updated content; saving it is up to the caller.
		onApply: (result: ComposeRefsResult) => Promise<void> | void;
	} = $props();

	const selection = new SvelteSet<string>();
	let applying = $state(false);
	let error = $state<string | null>(null);

	async function apply() {
		applying = true;
		error = null;
		const envKeys = env === null ? [] : selectionToEnvKeys(selection);
		const response = await tryCatch(
			secretSourceService.addComposeRefs(compose, selectionToAssignments(selection), { content: env ?? '', keys: envKeys })
		);
		if (response.error) {
			applying = false;
			error = extractApiErrorMessage(response.error);
			return;
		}
		await onApply(response.data);
		applying = false;
		if (response.data.skipped.length > 0) {
			toast.warning(m.compose_refs_skipped({ count: response.data.skipped.length }), {
				description: response.data.skipped.map((ref) => `${ref.service}/${ref.key}: ${ref.reason}`).join('\n')
			});
		}
		open = false;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={m.compose_refs_title()}
	description={m.compose_refs_description({ example: 'KEY: ${KEY}' })}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="grid gap-4 py-6">
			<ComposeRefsPicker {compose} {keys} {selection} {env} {composeLocked} />
			{#if error}
				<Alert.Root variant="destructive-subtle" icon={AlertIcon} heading={m.compose_refs_failed()} description={error} />
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={applying}
			submitAction="save"
			submitLabel={selection.size === 1 ? m.compose_refs_apply_one() : m.compose_refs_apply({ count: selection.size })}
			submitDisabled={selection.size === 0 || applying}
			submitLoading={applying}
			onSubmit={() => void apply()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
