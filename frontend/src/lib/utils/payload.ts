export function changedFields<T extends object, K extends keyof T>(original: T, next: Pick<T, K>): Partial<Pick<T, K>> {
	return Object.fromEntries(
		Object.entries(next).filter(([key, value]) => JSON.stringify(value) !== JSON.stringify(original[key as keyof T]))
	) as Partial<Pick<T, K>>;
}
