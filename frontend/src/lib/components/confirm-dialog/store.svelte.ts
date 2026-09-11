import { m } from '#lib/paraglide/messages.js';
import type { ConfirmDialogOptions, ConfirmDialogState } from '#lib/types/confirm-dialog.js';

export const confirmDialogState = $state<ConfirmDialogState>({ current: null });

export function openConfirmDialog({ title, message, confirm, checkboxes }: ConfirmDialogOptions) {
	confirmDialogState.current = {
		open: true,
		checkboxStates: Object.fromEntries((checkboxes ?? []).map((checkbox) => [checkbox.id, Boolean(checkbox.initialState)])),
		title,
		message,
		confirm: {
			label: confirm.label ?? m.common_confirm(),
			destructive: confirm.destructive ?? false,
			button: confirm.button,
			action: confirm.action
		},
		checkboxes
	};
}
