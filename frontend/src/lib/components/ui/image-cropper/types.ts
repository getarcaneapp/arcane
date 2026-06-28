import type { AvatarRootProps, DialogContentProps, WithChildren, WithoutChild, WithoutChildren } from 'bits-ui';
import type { Snippet } from 'svelte';
import type { CropperProps } from 'svelte-easy-crop';
import type { HTMLAttributes, HTMLButtonAttributes, HTMLInputAttributes } from 'svelte/elements';

export type ImageCropperRootPropsWithoutHTML = WithChildren<{
	id?: string;
	src?: string;
	onCropped?: (url: string) => void;
	onError?: (error: Error) => void;
	onUnsupportedFile?: (file: File) => void;
}>;

export type ImageCropperRootProps = ImageCropperRootPropsWithoutHTML & HTMLInputAttributes;

export type ImageCropperDialogProps = DialogContentProps;

export type ImageCropperCropperProps = Omit<Partial<CropperProps>, 'oncropcomplete' | 'image'>;

export type ImageCropperControlsWithoutHTML = WithChildren<{
	ref?: HTMLDivElement | null;
}>;

export type ImageCropperControlsProps = ImageCropperControlsWithoutHTML & WithoutChildren<HTMLAttributes<HTMLDivElement>>;

export type ImageCropperPreviewPropsWithoutHTML = {
	child?: Snippet<[{ src: string }]>;
};

export type ImageCropperPreviewProps = ImageCropperPreviewPropsWithoutHTML & WithoutChild<AvatarRootProps>;

export type ImageCropperUploadTriggerPropsWithoutHTML = WithChildren<{
	ref?: HTMLButtonElement | null;
}>;

export type ImageCropperUploadTriggerProps = ImageCropperUploadTriggerPropsWithoutHTML & WithoutChildren<HTMLButtonAttributes>;
