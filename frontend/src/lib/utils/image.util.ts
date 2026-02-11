export function getApplicationLogo(full = false, colorOverride?: string, version?: string): string {
	const params = new URLSearchParams();

	if (full) {
		params.set('full', 'true');
	}

	if (colorOverride) {
		params.set('color', colorOverride);
	}

	if (version) {
		params.set('v', version);
	}

	const query = params.toString();
	return query ? `/api/app-images/logo?${query}` : '/api/app-images/logo';
}

export function getDefaultProfilePicture(): string {
	return '/api/app-images/profile';
}
