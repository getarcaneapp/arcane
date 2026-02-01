import Root from './GenericFileBrowser.svelte';
import Breadcrumb from './FileBreadcrumb.svelte';
import List from './FileList.svelte';
import Preview from './FilePreview.svelte';
import UploadDialog from './FileUploadDialog.svelte';
import CreateFolderDialog from './CreateFolderDialog.svelte';
import VolumeBrowser from './VolumeBrowser.svelte';

export type { FileProvider } from './GenericFileBrowser.svelte';

export {
	Root,
	Breadcrumb,
	List,
	Preview,
	UploadDialog,
	CreateFolderDialog,
	VolumeBrowser,
	// aliases
	Root as FileBrowser,
	Breadcrumb as FileBreadcrumb,
	List as FileList,
	Preview as FilePreview,
	UploadDialog as FileUploadDialog,
	CreateFolderDialog as FileCreateFolderDialog
};
