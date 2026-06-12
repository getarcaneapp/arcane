type CategoryLike = {
	title: string;
	url: string;
};

export function orderCategoriesByNav<T extends CategoryLike>(categories: T[], navUrls: string[]): T[] {
	const categoriesByUrl = new Map(categories.map((category) => [category.url, category]));
	const orderedCategories = navUrls.map((url) => categoriesByUrl.get(url)).filter((category): category is T => Boolean(category));
	const unmatchedCategories = categories
		.filter((category) => !navUrls.includes(category.url))
		.sort((a, b) => a.title.localeCompare(b.title));

	return [...orderedCategories, ...unmatchedCategories];
}

export function getCategoryIcon<T>(iconMap: Record<string, T>, iconName: string | null | undefined, fallback: T): T {
	return iconMap[iconName ?? ''] ?? fallback;
}
