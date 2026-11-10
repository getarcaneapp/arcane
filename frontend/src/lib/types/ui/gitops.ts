import type { GitOpsSync, GitRepository } from '../gitops';
import type { CreateRequest, UpdateRequest } from './pagination';

export type GitRepositoryWritable =
	| 'name'
	| 'url'
	| 'provider'
	| 'authType'
	| 'username'
	| 'sshHostKeyVerification'
	| 'commitAuthorName'
	| 'commitAuthorEmail'
	| 'description'
	| 'enabled'
	| 'token'
	| 'sshKey'
	| 'signingKey'
	| 'signingKeyPassphrase';

export type GitRepositoryCreate = CreateRequest<GitRepository, GitRepositoryWritable, 'name' | 'url' | 'authType'>;

export type GitRepositoryUpdate = UpdateRequest<GitRepository, GitRepositoryWritable>;

export type GitOpsSyncWritable =
	| 'name'
	| 'repositoryId'
	| 'branch'
	| 'composePath'
	| 'targetType'
	| 'mode'
	| 'projectId'
	| 'projectName'
	| 'backupDirectory'
	| 'backupPaths'
	| 'backupOnSave'
	| 'autoSync'
	| 'syncInterval'
	| 'syncDirectory'
	| 'pullImageAfterSync'
	| 'redeployAfterSync'
	| 'maxSyncFiles'
	| 'maxSyncTotalSize'
	| 'maxSyncBinarySize'
	| 'preDeployScriptPath'
	| 'preDeployRunnerImage'
	| 'preDeployEnv'
	| 'preDeployExtraMounts'
	| 'preDeployTimeoutSec'
	| 'preDeployNetworkMode';

export type GitOpsSyncCreate = CreateRequest<GitOpsSync, GitOpsSyncWritable, 'name' | 'repositoryId' | 'branch'>;

export type GitOpsSyncUpdate = UpdateRequest<GitOpsSync, GitOpsSyncWritable>;
