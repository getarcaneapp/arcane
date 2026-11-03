import type { Paginated } from '../base';
import type {
	SwarmConfig,
	SwarmConfigUpdateRequest,
	SwarmNode,
	SwarmSecret,
	SwarmSecretUpdateRequest,
	SwarmServiceSummary,
	SwarmStackSource,
	SwarmStackSummary,
	SwarmTaskSummary
} from '../swarm';
import type { UpdateRequest } from './pagination';

export interface RawSwarmServicePort {
	protocol?: string;
	Protocol?: string;
	targetPort?: number;
	TargetPort?: number;
	publishedPort?: number;
	PublishedPort?: number;
	publishMode?: string;
	PublishMode?: string;
}

export interface RawSwarmServiceMount {
	type?: string;
	Type?: string;
	source?: string;
	Source?: string;
	target?: string;
	Target?: string;
	readOnly?: boolean;
	ReadOnly?: boolean;
	volumeDriver?: string;
	VolumeDriver?: string;
	volumeOptions?: Record<string, string>;
	VolumeOptions?: Record<string, string>;
	devicePath?: string;
	DevicePath?: string;
}

export interface ServiceNetworkAttachment {
	target: string;
	aliases: string[];
}

export interface RawServiceNetworkAttachment {
	Target?: string;
	target?: string;
	Aliases?: string[];
	aliases?: string[];
}

export interface ServiceVirtualIP {
	networkID: string;
	addr: string;
}

export interface RawServiceVirtualIP {
	NetworkID?: string;
	networkID?: string;
	Addr?: string;
	addr?: string;
}

export type SwarmNodeWritable = 'name' | 'version' | 'labels' | 'role' | 'availability';

export type SwarmNodeUpdate = UpdateRequest<SwarmNode, SwarmNodeWritable>;

export type SwarmStackSourceWritable = 'composeContent' | 'overrideContent' | 'envContent' | 'files';

export type SwarmStackSourceUpdate = UpdateRequest<SwarmStackSource, SwarmStackSourceWritable>;

export type SwarmKvKind = 'secrets' | 'configs';

export type SwarmKvCreateRequest = Pick<SwarmConfig, 'spec'>;

export type SwarmKvSummary = SwarmConfig | SwarmSecret;

export type SwarmKvUpdateRequest = SwarmConfigUpdateRequest | SwarmSecretUpdateRequest;

export type SwarmServicesPaginatedResponse = Paginated<SwarmServiceSummary>;

export type SwarmNodesPaginatedResponse = Paginated<SwarmNode>;

export type SwarmTasksPaginatedResponse = Paginated<SwarmTaskSummary>;

export type SwarmStacksPaginatedResponse = Paginated<SwarmStackSummary>;
