import type { IPAM } from 'dockerode';

export interface NetworkCreateDto {
	Driver?: string;
	CheckDuplicate?: boolean;
	Internal?: boolean;
	Attachable?: boolean;
	Ingress?: boolean;
	IPAM?: IPAM;
	EnableIPv6?: boolean;
	Options?: Record<string, string>;
	Labels?: Record<string, string>;
}

export interface NetworkCreateRequest {
	name: string;
	options: NetworkCreateDto;
}

export interface NetworkUsageCounts {
	networksInuse: number;
	networksUnused: number;
	totalNetworks: number;
}

export interface ContainerEndpointDto {
	Name: string;
	EndpointID: string;
	MacAddress: string;
	IPv4Address: string;
	IPv6Address: string;
}

export interface IPAMSubnetDto {
	Subnet: string;
	Gateway?: string;
	IPRange?: string;
	// Support both keys we see in Docker variants
	AuxAddress?: Record<string, string>;
	AuxiliaryAddresses?: Record<string, string>;
}

export interface IPAMDto {
	Driver: string;
	Options?: Record<string, string>;
	Config?: IPAMSubnetDto[];
}

export interface NetworkSummaryDto {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string; // ISO RFC3339 string
	options?: Record<string, string> | null;
	labels?: Record<string, string> | null;
	inUse: boolean;
	isDefault?: boolean;
}

export interface NetworkInspectDto {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	options?: Record<string, string> | null;
	labels?: Record<string, string> | null;
	containers?: Record<string, ContainerEndpointDto> | null;
	ipam?: IPAMDto;
	internal: boolean;
	attachable: boolean;
	ingress: boolean;
	enableIPv6?: boolean;
}
