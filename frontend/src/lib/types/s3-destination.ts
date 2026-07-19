export type S3Destination = {
	id: string;
	name: string;
	endpoint?: string;
	bucket: string;
	region: string;
	accessKeyId: string;
	prefix?: string;
	useSsl: boolean;
	forcePathStyle: boolean;
	secretConfigured: boolean;
	createdAt: string;
	updatedAt?: string;
};

export type CreateS3Destination = {
	name: string;
	endpoint: string;
	bucket: string;
	region: string;
	accessKeyId: string;
	secretAccessKey: string;
	prefix: string;
	useSsl: boolean;
	forcePathStyle: boolean;
};

export type UpdateS3Destination = CreateS3Destination;
