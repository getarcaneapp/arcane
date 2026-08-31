import type { ActionButton } from '#lib/layouts';

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
			label: create.label,
			onclick: create.onclick
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
