import type { ActionButton } from '$lib/layouts';

type CreateRefreshActionOptions = {
	canCreate: boolean;
	createLabel: string;
	onCreate: () => void;
	refreshLabel: string;
	onRefresh: () => void | Promise<void>;
	refreshing: boolean;
};

export function createRefreshActionButtons({
	canCreate,
	createLabel,
	onCreate,
	refreshLabel,
	onRefresh,
	refreshing
}: CreateRefreshActionOptions): ActionButton[] {
	const buttons: ActionButton[] = [];
	if (canCreate) {
		buttons.push({
			id: 'create',
			action: 'create',
			label: createLabel,
			onclick: onCreate
		});
	}
	buttons.push({
		id: 'refresh',
		action: 'restart',
		label: refreshLabel,
		onclick: onRefresh,
		loading: refreshing,
		disabled: refreshing
	});
	return buttons;
}
