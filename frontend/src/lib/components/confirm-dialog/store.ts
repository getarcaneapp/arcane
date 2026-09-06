import { writable } from 'svelte/store';
import { m } from '#lib/paraglide/messages.js';
import type { ConfirmDialogOptions } from '#lib/types/confirm-dialog.js';

export const confirmDialogStore = writable<ConfirmDialogOptions & { open: boolean }>({
	open: false,
	title: '',
	message: '',
	confirm: {
		label: m.common_confirm(),
		destructive: false,
		action: () => {}
	}
});

export function openConfirmDialog({ title, message, confirm, checkboxes }: ConfirmDialogOptions) {
	confirmDialogStore.update(() => ({
		open: true,
		title,
		message,
		confirm: {
			label: confirm.label ?? m.common_confirm(),
			destructive: confirm.destructive ?? false,
			button: confirm.button,
			action: confirm.action
		},
		checkboxes
	}));
}
