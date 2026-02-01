<script lang="ts">
	import { volumeBrowserService } from '$lib/services/volume-browser-service';
	import { volumeBackupService } from '$lib/services/volume-backup-service';
	import GenericFileBrowser, { type FileProvider } from './GenericFileBrowser.svelte';

	let { volumeName }: { volumeName: string } = $props();

	const provider: FileProvider = {
		list: (path) => volumeBrowserService.listDirectory(volumeName, path),
		mkdir: (path) => volumeBrowserService.createDirectory(volumeName, path),
		upload: (path, file) => volumeBrowserService.uploadFile(volumeName, path, file),
		delete: (path) => volumeBrowserService.deleteFile(volumeName, path),
		download: (path) => volumeBrowserService.downloadFile(volumeName, path),
		getContent: (path) => volumeBrowserService.getFileContent(volumeName, path),
		listBackups: async () => {
			const res = await volumeBackupService.listBackups(volumeName, {
				pagination: { page: 1, limit: 200 },
				sort: { column: 'createdAt', direction: 'desc' }
			});
			return res.data;
		},
		restoreFromBackup: (backupId, path) => volumeBackupService.restoreBackupFiles(volumeName, backupId, [path]),
		backupHasPath: (backupId, path) => volumeBackupService.backupHasPath(backupId, path)
	};
</script>

<GenericFileBrowser {provider} rootLabel={volumeName} persistKey="volume-file-browser" />
