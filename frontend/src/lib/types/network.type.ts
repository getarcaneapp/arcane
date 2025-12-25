export interface IPAMConfig {
	subnet?: string;
	gateway?: string;
	ipRange?: string;
	auxAddress?: Record<string, string>;
}

export interface IPAM {
	driver?: string;
	options?: Record<string, string>;
	config?: IPAMConfig[];
}

export interface NetworkCreateOptions {
	driver?: string;
	checkDuplicate?: boolean;
	internal?: boolean;
	attachable?: boolean;
	ingress?: boolean;
	ipam?: IPAM;
	enableIPv6?: boolean;
	options?: Record<string, string>;
	labels?: Record<string, string>;
}

// Request sent to backend
export interface NetworkCreateRequest {
	name: string;
	options: NetworkCreateOptions;
}

export interface NetworkUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface ContainerEndpointDto {
	id?: string;
	name: string;
	endpointId: string;
	macAddress: string;
	ipv4Address: string;
	ipv6Address: string;
}

export interface IPAMSubnetDto {
	subnet: string;
	gateway?: string;
	ipRange?: string;
	auxAddress?: Record<string, string>;
}

export interface IPAMDto {
	driver: string;
	options?: Record<string, string>;
	config?: IPAMSubnetDto[];
}

export interface NetworkSummaryDto {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	options?: Record<string, string> | null;
	labels?: Record<string, string> | null;
	inUse: boolean;
	isDefault?: boolean;
}

export interface ConfigReference {
	Network?: string;
}

export interface PeerInfo {
	Name?: string;
	IP?: string;
}

export interface Task {
	Name?: string;
	EndpointID?: string;
	EndpointIP?: string;
	Info?: Record<string, string>;
}

export interface ServiceInfo {
	VIP?: string;
	Ports?: string[];
	LocalLBIndex?: number;
	Tasks?: Task[];
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
	containersList?: ContainerEndpointDto[];
	ipam?: IPAMDto;
	internal: boolean;
	attachable: boolean;
	ingress: boolean;
	enableIPv6?: boolean;
	enableIPv4?: boolean;
	configFrom?: ConfigReference;
	configOnly?: boolean;
	peers?: PeerInfo[];
	services?: Record<string, ServiceInfo>;
}
