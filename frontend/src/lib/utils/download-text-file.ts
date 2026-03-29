export function downloadTextFile(filename: string, content: string, mimeType = 'application/x-pem-file'): void {
	const blob = new Blob([content], { type: `${mimeType};charset=utf-8` });
	const url = window.URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = url;
	link.setAttribute('download', filename);
	document.body.appendChild(link);
	link.click();
	document.body.removeChild(link);
	window.URL.revokeObjectURL(url);
}
