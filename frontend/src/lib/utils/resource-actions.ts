import type { ActionButton } from '#lib/components/action-button-group/types.js';

type CreateRefreshActionOptions = {
	create?: {
		allowed: boolean;
		label: string;
		onclick: () => void;
	};
	refreshLabel: string;
	onRefresh: () => void | Promise<void>;
	refreshing: boolean;
};

export function createRefreshActionButtons({
	create,
	refreshLabel,
	onRefresh,
	refreshing
}: CreateRefreshActionOptions): ActionButton[] {
	const buttons: ActionButton[] = [];
	if (create?.allowed) {
		buttons.push({
			id: 'create',
			action: 'create',
			placement: 'primary',
			label: create.label,
			onclick: create.onclick
		});
	}
	buttons.push({
		id: 'refresh',
		action: 'refresh',
		placement: 'secondary',
		iconOnly: true,
		label: refreshLabel,
		onclick: onRefresh,
		loading: refreshing,
		disabled: refreshing
	});
	return buttons;
}
