import * as m from '#lib/paraglide/messages.js';

import FileDropZone from './file-drop-zone.svelte';
import { type FileRejectedReason, type FileDropZoneProps } from './types';
export { BYTE, KILOBYTE, MEGABYTE, GIGABYTE, displaySize } from './display-size';

// utilities for limiting accepted files
export const ACCEPT_IMAGE = 'image/*';
export const ACCEPT_VIDEO = 'video/*';
export const ACCEPT_AUDIO = 'audio/*';

/** The rejection reason in the user's language. */
export function fileRejectedMessage(reason: FileRejectedReason): string {
	switch (reason) {
		case 'Maximum file size exceeded':
			return m.file_too_large();
		case 'File type not allowed':
			return m.unsupported_file_type();
		case 'Maximum files uploaded':
			return m.too_many_files();
	}
}

export { FileDropZone, type FileRejectedReason, type FileDropZoneProps };
