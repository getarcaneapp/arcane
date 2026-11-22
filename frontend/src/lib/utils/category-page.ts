import type { NormalizedCategory } from '#lib/components/category-index-page.types.js';
import type { IconType } from '#lib/icons/index.js';

export function normalizeCategory(
	category: { id: string; title: string; description: string; icon: string; url: string },
	messages: { title: () => string; description: () => string } | undefined,
	iconMap: Record<string, IconType>,
	fallbackIcon: IconType
): NormalizedCategory {
	return {
		id: category.id,
		title: messages?.title() ?? category.title,
		description: messages?.description() ?? category.description,
		icon: iconMap[category.icon ?? ''] ?? fallbackIcon,
		href: category.url
	};
}
