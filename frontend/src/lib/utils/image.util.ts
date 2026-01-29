type SkipCacheUntil = {
	[key: string]: number;
};

export function getApplicationLogo(full = false, colorOverride?: string): string {
	// Build base URL - don't include color param for normal requests
	// The backend reads the saved color from settings automatically
	// Only include color param when explicitly previewing a different color
	const baseUrl = full ? '/api/app-images/logo?full=true' : '/api/app-images/logo';

	if (colorOverride) {
		const separator = full ? '&' : '?';
		return `${baseUrl}${separator}color=${encodeURIComponent(colorOverride)}`;
	}

	return getCachedImageUrl(baseUrl);
}

export function bustLogoCache(): void {
	// Set skip cache for all logo URL patterns to force refetch
	const skipCacheUntil = Date.now() + 60000; // Skip cache for 1 minute
	const skipCacheUntilMap: SkipCacheUntil = JSON.parse(localStorage.getItem('skip-cache-until') ?? '{}');

	// Clear all existing logo cache entries and set new ones
	for (const key of Object.keys(skipCacheUntilMap)) {
		delete skipCacheUntilMap[key];
	}

	// Add cache bust timestamp for base logo URLs
	skipCacheUntilMap[hashKey('/api/app-images/logo')] = skipCacheUntil;
	skipCacheUntilMap[hashKey('/api/app-images/logo?full=true')] = skipCacheUntil;

	localStorage.setItem('skip-cache-until', JSON.stringify(skipCacheUntilMap));
}

export function getDefaultProfilePicture(): string {
	return getCachedImageUrl('/api/app-images/profile');
}

function getCachedImageUrl(url: string) {
	const skipCacheUntil = getSkipCacheUntil(url);
	const skipCache = skipCacheUntil > Date.now();
	if (skipCache) {
		const separator = url.includes('?') ? '&' : '?';
		url += separator + 'skip-cache=' + skipCacheUntil.toString();
	}

	return url.toString();
}

function getSkipCacheUntil(url: string) {
	const skipCacheUntil: SkipCacheUntil = JSON.parse(localStorage.getItem('skip-cache-until') ?? '{}');
	return skipCacheUntil[hashKey(url)] ?? 0;
}

function hashKey(key: string): string {
	let hash = 0;
	for (let i = 0; i < key.length; i++) {
		const char = key.charCodeAt(i);
		hash = (hash << 5) - hash + char;
		hash = hash & hash;
	}
	return Math.abs(hash).toString(36);
}
