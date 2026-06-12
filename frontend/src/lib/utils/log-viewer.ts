type LogViewerHandle = {
	startLogStream: () => void;
	stopLogStream: () => void;
	clearLogs: (options: { hard: boolean; restart: boolean }) => Promise<void>;
};

export function startLogViewerStream(viewer: LogViewerHandle | undefined) {
	viewer?.startLogStream();
}

export function stopLogViewerStream(viewer: LogViewerHandle | undefined) {
	viewer?.stopLogStream();
}

export async function refreshLogViewerStream(viewer: LogViewerHandle | undefined) {
	await viewer?.clearLogs({ hard: true, restart: true });
}
