import type { CropArea } from 'svelte-easy-crop';

export const getFileFromUrl = async (url: string, fileName = 'cropped'): Promise<File> => {
	const response = await fetch(url);

	if (!response.ok) {
		throw new Error(`failed to fetch cropped image: ${response.status} ${response.statusText}`);
	}

	const blob = await response.blob();
	return new File([blob], fileName, { type: blob.type });
};

const createImage = (url: string): Promise<HTMLImageElement> => {
	return new Promise<HTMLImageElement>((resolve, reject) => {
		const image = new Image();
		image.addEventListener('load', () => resolve(image));
		image.addEventListener('error', (error) => reject(error));
		image.setAttribute('crossOrigin', 'anonymous');
		image.src = url;
	});
};

const getRadianAngle = (degreeValue: number) => {
	return (degreeValue * Math.PI) / 180;
};

export const getCroppedImg = async (imageSrc: string, pixelCrop: CropArea, rotation = 0): Promise<string> => {
	const image = await createImage(imageSrc);
	const canvas = document.createElement('canvas');
	const ctx = canvas.getContext('2d');

	if (!ctx) {
		throw new Error('failed to get 2d rendering context');
	}

	const maxSize = Math.max(image.width, image.height);
	const safeArea = 2 * ((maxSize / 2) * Math.sqrt(2));

	canvas.width = safeArea;
	canvas.height = safeArea;

	ctx.translate(safeArea / 2, safeArea / 2);
	ctx.rotate(getRadianAngle(rotation));
	ctx.translate(-safeArea / 2, -safeArea / 2);
	ctx.drawImage(image, safeArea / 2 - image.width * 0.5, safeArea / 2 - image.height * 0.5);

	const data = ctx.getImageData(0, 0, safeArea, safeArea);

	canvas.width = pixelCrop.width;
	canvas.height = pixelCrop.height;

	ctx.putImageData(
		data,
		Math.round(0 - safeArea / 2 + image.width * 0.5 - pixelCrop.x),
		Math.round(0 - safeArea / 2 + image.height * 0.5 - pixelCrop.y)
	);

	return new Promise((resolve, reject) => {
		canvas.toBlob(
			(file) => {
				if (!file) {
					reject(new Error('failed to crop image'));
					return;
				}

				resolve(URL.createObjectURL(file));
			},
			'image/jpeg',
			0.92
		);
	});
};
