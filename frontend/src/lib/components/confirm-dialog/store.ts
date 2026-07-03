import { writable } from 'svelte/store';
import { m } from '$lib/paraglide/messages';

interface ConfirmDialogStore {
	open: boolean;
	title: string;
	message: string;
	confirm: {
		label?: string;
		destructive?: boolean;
		action: (checkboxStates: Record<string, boolean>) => void;
	};
	checkboxes?: Array<{
		id: string;
		label: string;
		initialState?: boolean;
	}>;
}

export const confirmDialogStore = writable<ConfirmDialogStore>({
	open: false,
	title: '',
	message: '',
	confirm: {
		label: m.common_confirm(),
		destructive: false,
		action: () => {}
	}
});

export function openConfirmDialog({
	title,
	message,
	confirm,
	checkboxes
}: {
	title: string;
	message: string;
	confirm: {
		label?: string;
		destructive?: boolean;
		action: (checkboxStates: Record<string, boolean>) => void;
	};
	checkboxes?: Array<{
		id: string;
		label: string;
		initialState?: boolean;
	}>;
}) {
	confirmDialogStore.update(() => ({
		open: true,
		title,
		message,
		confirm: {
			label: confirm.label ?? m.common_confirm(),
			destructive: confirm.destructive ?? false,
			action: confirm.action
		},
		checkboxes
	}));
}
