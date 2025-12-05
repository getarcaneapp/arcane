export interface VolumeUsageData {
	size: number;
	refCount: number;
}

export interface VolumeCreateRequest {
	name: string;
	driver?: string;
	driverOpts?: Record<string, string>;
	labels?: Record<string, string>;
}

export interface VolumeSummaryDto {
	id: string;
	name: string;
	driver: string;
	mountpoint: string;
	scope: string;
	options: Record<string, string> | null;
	labels: Record<string, string>;
	createdAt: string;
	inUse: boolean;
	usageData?: VolumeUsageData;
	size: number;
}

export interface VolumeDetailDto extends VolumeSummaryDto {
	containers: string[];
}

export interface VolumeUsageDto {
	inUse: boolean;
	containers: string[];
}

export interface VolumeUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface VolumeSizeInfo {
	name: string;
	size: number;
	refCount: number;
}
