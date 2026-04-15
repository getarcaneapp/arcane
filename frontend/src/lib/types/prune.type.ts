export type PruneContainerMode = 'none' | 'stopped' | 'olderThan';
export type PruneImageMode = 'none' | 'dangling' | 'all' | 'olderThan';
export type PruneVolumeMode = 'none' | 'anonymous' | 'all';
export type PruneNetworkMode = 'none' | 'unused' | 'olderThan';
export type PruneBuildCacheMode = 'none' | 'unused' | 'all' | 'olderThan';

export interface PruneContainersOptions {
	mode: Exclude<PruneContainerMode, 'none'>;
	until?: string;
}

export interface PruneImagesOptions {
	mode: Exclude<PruneImageMode, 'none'>;
	until?: string;
}

export interface PruneVolumesOptions {
	mode: Exclude<PruneVolumeMode, 'none'>;
}

export interface PruneNetworksOptions {
	mode: Exclude<PruneNetworkMode, 'none'>;
	until?: string;
}

export interface PruneBuildCacheOptions {
	mode: Exclude<PruneBuildCacheMode, 'none'>;
	until?: string;
}

export interface SystemPruneRequest {
	containers?: PruneContainersOptions;
	images?: PruneImagesOptions;
	volumes?: PruneVolumesOptions;
	networks?: PruneNetworksOptions;
	buildCache?: PruneBuildCacheOptions;
}
