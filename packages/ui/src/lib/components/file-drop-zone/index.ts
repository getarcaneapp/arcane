import FileDropZone from './file-drop-zone.svelte';
import { type FileRejectedReason, type FileDropZoneProps } from './types';
export { BYTE, KILOBYTE, MEGABYTE, GIGABYTE, displaySize } from './display-size';

// utilities for limiting accepted files
export const ACCEPT_IMAGE = 'image/*';
export const ACCEPT_VIDEO = 'video/*';
export const ACCEPT_AUDIO = 'audio/*';

export { FileDropZone, type FileRejectedReason, type FileDropZoneProps };
