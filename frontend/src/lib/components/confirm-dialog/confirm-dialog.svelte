<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { confirmDialogState } from './store.svelte.js';
	const dialog = $derived(confirmDialogState.current);
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import Checkbox from '../ui/checkbox/checkbox.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';

	async function handleConfirm() {
		if (!dialog) return;
		const action = dialog.confirm.action;
		const states = $state.snapshot(dialog.checkboxStates);
		dialog.open = false;
		// Keep synchronous action failures inside the promise boundary.
		const result = await tryCatch(Promise.resolve().then(() => action(states)));
		if (result.error !== null) {
			console.error('Confirm action failed:', result.error);
			toast.error(m.unexpected_error());
		}
	}
</script>

{#if dialog}
	<Dialog.Root bind:open={dialog.open}>
		<Dialog.Content
			class="z-[var(--arcane-z-critical-surface)] w-full max-w-md sm:max-w-lg"
			overlayClass="z-[var(--arcane-z-critical-surface)]"
		>
			<Dialog.Header class="space-y-3">
				<Dialog.Title class="flex items-start gap-3 text-lg leading-tight font-semibold">
					<AlertIcon class="mt-0.5 size-5 shrink-0 text-destructive" />
					<span class="min-w-0 break-words">
						{dialog.title}
					</span>
				</Dialog.Title>
			</Dialog.Header>

			<div class="mt-4 min-w-0 text-sm leading-relaxed break-words whitespace-pre-wrap text-muted-foreground">
				{dialog.message}
			</div>

			{#if dialog.checkboxes && dialog.checkboxes.length > 0}
				<div class="mt-6 flex flex-col gap-4 border-t border-border pt-4">
					{#each dialog.checkboxes as checkbox (checkbox.id)}
						<div class="flex items-start space-x-3">
							{#if dialog.checkboxStates[checkbox.id] !== undefined}
								<Checkbox
									id={checkbox.id}
									bind:checked={dialog.checkboxStates[checkbox.id]}
									aria-labelledby={`${checkbox.id}-label`}
									class="mt-0.5"
								/>
							{:else}
								<Checkbox
									id={checkbox.id}
									checked={false}
									onchange={() => (dialog.checkboxStates[checkbox.id] = true)}
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

								{#if checkbox.id === 'files' && dialog.checkboxStates[checkbox.id]}
									<div class="mt-1 text-xs leading-snug text-destructive">{m.confirm_remove_project_files_warning()}</div>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}

			<Dialog.Footer class="mt-6">
				<div class="flex w-full justify-end gap-3">
					<ArcaneButton class="min-w-[80px]" action="cancel" onclick={() => (dialog.open = false)} />
					<ArcaneButton
						class="min-w-[80px]"
						action={dialog.confirm.button ?? (dialog.confirm.destructive ? 'remove' : 'confirm')}
						customLabel={dialog.confirm.label}
						onclick={handleConfirm}
					/>
				</div>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
