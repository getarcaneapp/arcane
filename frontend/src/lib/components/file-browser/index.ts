import Breadcrumb from './FileBreadcrumb.svelte';
import List from './FileList.svelte';
import Preview from './FilePreview.svelte';
import UploadDialog from './FileUploadDialog.svelte';
import CreateFolderDialog from './CreateFolderDialog.svelte';

export type { FileProvider } from './file-provider';
export { sortFileEntries } from './file-provider';

export {
	Breadcrumb,
	List,
	Preview,
	UploadDialog,
	CreateFolderDialog,
	// aliases
	Breadcrumb as FileBreadcrumb,
	List as FileList,
	Preview as FilePreview,
	UploadDialog as FileUploadDialog,
	CreateFolderDialog as FileCreateFolderDialog
};
