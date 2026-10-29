import { m } from '#lib/paraglide/messages.js';

export function inUseBadge(visible: boolean) {
	return (item: { inUse?: boolean }) => {
		if (!visible) return null;
		if (item.inUse) return { variant: 'green' as const, text: m.common_in_use() };
		return { variant: 'amber' as const, text: m.common_unused() };
	};
}
