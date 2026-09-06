export type DeployProjectOptions = {
	pullPolicy?: 'missing' | 'always' | 'never';
	forceRecreate?: boolean;
	removeOrphans?: boolean;
	recreateVolumes?: boolean;
};
