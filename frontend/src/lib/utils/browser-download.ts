export function downloadBlob(data: BlobPart, filename: string): void {
	const url = window.URL.createObjectURL(new Blob([data]));
	const link = document.createElement('a');
	link.href = url;
	link.setAttribute('download', filename);
	document.body.appendChild(link);
	link.click();
	link.remove();
	window.URL.revokeObjectURL(url);
}

export function filenameFromPath(path: string, fallback = 'download'): string {
	return path.split('/').pop() || fallback;
}
