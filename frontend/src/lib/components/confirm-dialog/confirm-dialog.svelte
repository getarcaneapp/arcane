<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { confirmDialogStore } from './store';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import Checkbox from '../ui/checkbox/checkbox.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';

	let checkboxStates = $state<Record<string, boolean>>({});

	$effect(() => {
		if ($confirmDialogStore.open && $confirmDialogStore.checkboxes) {
			const newStates: Record<string, boolean> = {};

			for (const checkbox of $confirmDialogStore.checkboxes) {
				newStates[checkbox.id] = Boolean(checkbox.initialState);
			}

			checkboxStates = newStates;
		}
	});

	function handleConfirm() {
		const action = $confirmDialogStore.confirm.action;
		const states = $state.snapshot(checkboxStates);
		$confirmDialogStore.open = false;
		// The dialog is already closed, so a throwing action would otherwise be an
		// unhandled rejection the user never sees — surface it instead. Invoke
		// inside the chain so synchronous throws also land in the catch.
		Promise.resolve()
			.then(() => action(states))
			.catch((error) => {
				console.error('Confirm action failed:', error);
				toast.error(m.unexpected_error());
			});
	}
</script>

<Dialog.Root bind:open={$confirmDialogStore.open}>
	<Dialog.Content
		class="z-[var(--arcane-z-critical-surface)] w-full max-w-md sm:max-w-lg"
		overlayClass="z-[var(--arcane-z-critical-surface)]"
	>
		<Dialog.Header class="space-y-3">
			<Dialog.Title class="flex items-start gap-3 text-lg leading-tight font-semibold">
				<AlertIcon class="mt-0.5 size-5 shrink-0 text-destructive" />
				<span class="min-w-0 break-words">
					{$confirmDialogStore.title}
				</span>
			</Dialog.Title>
		</Dialog.Header>

		<div class="mt-4 min-w-0 text-sm leading-relaxed break-words whitespace-pre-wrap text-muted-foreground">
			{$confirmDialogStore.message}
		</div>

		{#if $confirmDialogStore.checkboxes && $confirmDialogStore.checkboxes.length > 0}
			<div class="mt-6 flex flex-col gap-4 border-t border-border pt-4">
				{#each $confirmDialogStore.checkboxes as checkbox (checkbox.id)}
					<div class="flex items-start space-x-3">
						{#if checkboxStates[checkbox.id] !== undefined}
							<Checkbox
								id={checkbox.id}
								bind:checked={checkboxStates[checkbox.id]}
								aria-labelledby={`${checkbox.id}-label`}
								class="mt-0.5"
							/>
						{:else}
							<Checkbox
								id={checkbox.id}
								checked={false}
								onchange={() => (checkboxStates[checkbox.id] = true)}
								aria-labelledby={`${checkbox.id}-label`}
								class="mt-0.5"
							/>
						{/if}

						<div class="min-w-0">
							<Label
								id={`${checkbox.id}-label`}
								for={checkbox.id}
								class="min-w-0 text-sm leading-relaxed font-medium break-words peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
							>
								{checkbox.label}
							</Label>

							{#if checkbox.id === 'files' && checkboxStates[checkbox.id]}
								<div class="mt-1 text-xs leading-snug text-destructive">{m.confirm_remove_project_files_warning()}</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}

		<Dialog.Footer class="mt-6">
			<div class="flex w-full justify-end gap-3">
				<ArcaneButton class="min-w-[80px]" action="cancel" onclick={() => ($confirmDialogStore.open = false)} />
				<ArcaneButton
					class="min-w-[80px]"
					action={$confirmDialogStore.confirm.button ?? ($confirmDialogStore.confirm.destructive ? 'remove' : 'confirm')}
					customLabel={$confirmDialogStore.confirm.label}
					onclick={handleConfirm}
				/>
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
