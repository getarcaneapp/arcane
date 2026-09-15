export function downloadFromUrl(url: string, filename = ''): void {
	const link = document.createElement('a');
	link.href = url;
	link.setAttribute('download', filename);
	document.body.appendChild(link);
	link.click();
	link.remove();
}

export function downloadBlob(data: BlobPart, filename: string): void {
	const url = window.URL.createObjectURL(new Blob([data]));
	downloadFromUrl(url, filename);
	window.URL.revokeObjectURL(url);
}

export function filenameFromPath(path: string, fallback = 'download'): string {
	return path.split('/').pop() || fallback;
}

export function filenameFromContentDisposition(header: string | null, fallback: string): string {
	return /filename="?([^";]+)"?/.exec(header ?? '')?.[1] || fallback;
}
