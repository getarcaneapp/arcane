export function capitalizeFirstLetter(string: string): string {
	if (!string) return '';
	return string.charAt(0).toUpperCase() + string.slice(1);
}

export function shortId(id: string | undefined, length = 12): string {
	if (!id) return 'N/A';
	return id.substring(0, length);
}

export function truncateString(str: string | undefined, maxLength: number): string {
	if (!str) return '';
	if (str.length <= maxLength) {
		return str;
	}
	return str.substring(0, maxLength - 3) + '...';
}

export function stripImageDigest(image: string): string {
	return image.replace(/@sha256:[a-f0-9]+/g, '');
}
