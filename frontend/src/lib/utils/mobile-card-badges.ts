import { m } from '$lib/paraglide/messages';

export function inUseBadge(visible: boolean) {
	return (item: { inUse?: boolean }) =>
		visible
			? item.inUse
				? { variant: 'green' as const, text: m.common_in_use() }
				: { variant: 'amber' as const, text: m.common_unused() }
			: null;
}
