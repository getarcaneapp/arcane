export type UiLabels = {
	loading: () => string;
	close: () => string;
	more: () => string;
	morePages: () => string;
	fileDropZoneDragDrop: () => string;
	fileDropZoneMaxFiles: (maxFiles: string) => string;
	fileDropZoneUpToEach: (maxSize: string) => string;
	fileDropZoneMaxSize: (maxSize: string) => string;
};

let labels: UiLabels = {
	loading: () => 'Loading…',
	close: () => 'Close',
	more: () => 'More',
	morePages: () => 'More pages',
	fileDropZoneDragDrop: () => "Drag 'n' drop files here, or click to select files",
	fileDropZoneMaxFiles: (maxFiles) => `You can upload ${maxFiles} files`,
	fileDropZoneUpToEach: (maxSize) => `(up to ${maxSize} each)`,
	fileDropZoneMaxSize: (maxSize) => `Maximum size ${maxSize}`
};

export function setUiLabels(next: UiLabels) {
	labels = next;
}

export function getUiLabels(): UiLabels {
	return labels;
}
