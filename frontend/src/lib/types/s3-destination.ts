import type { CreateRequest, UpdateRequest } from './shared';

export type S3Destination = {
	id: string;
	name: string;
	endpoint?: string;
	bucket: string;
	region: string;
	accessKeyId: string;
	secretAccessKey?: string;
	prefix?: string;
	useSsl: boolean;
	forcePathStyle: boolean;
	secretConfigured: boolean;
	createdAt: string;
	updatedAt?: string;
};

export type S3DestinationWritable =
	| 'name'
	| 'endpoint'
	| 'bucket'
	| 'region'
	| 'accessKeyId'
	| 'secretAccessKey'
	| 'prefix'
	| 'useSsl'
	| 'forcePathStyle';

export type S3DestinationCreate = CreateRequest<S3Destination, S3DestinationWritable, 'name' | 'bucket' | 'accessKeyId'>;

export type S3DestinationUpdate = UpdateRequest<S3Destination, S3DestinationWritable>;
