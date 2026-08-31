import { expect, test as setup, type Page } from '../fixtures/test.fixture';
import { readApiData } from '../utils/fetch.util';

const GITOPS_REPO_NAME = 'gitsyncs-test-repo';
const GITOPS_REPO_URL = 'https://github.com/getarcaneapp/gitsyncs.git';
const GITOPS_REPO_BRANCH = 'main';
const GITOPS_COMPOSE_PATH = 'compose-test-repo/compose.yaml';
const GITOPS_SYNC_NAME = 'gitops-test-sync';
const GITOPS_PROJECT_NAME = 'gitops-test-project';

type GitRepository = {
	id: string;
	name: string;
	url: string;
	authType: string;
	enabled: boolean;
};

type GitOpsSync = {
	id: string;
	name: string;
	projectId?: string;
	projectName: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	lastSyncCommit?: string;
};

type GitOpsProject = {
	id: string;
	name: string;
	gitOpsManagedBy?: string;
	lastSyncCommit?: string;
};

async function listRepositories(page: Page): Promise<GitRepository[]> {
	const response = await page.request.get(
		`/api/customize/git-repositories?search=${encodeURIComponent(GITOPS_REPO_NAME)}&start=0&limit=100`
	);
	return readApiData<GitRepository[]>(response, 'List Git repositories');
}

async function ensureRepository(page: Page): Promise<GitRepository> {
	const existing = (await listRepositories(page)).find(
		(repository) => repository.name === GITOPS_REPO_NAME
	);
	const body = {
		name: GITOPS_REPO_NAME,
		url: GITOPS_REPO_URL,
		authType: 'none',
		enabled: true
	};

	const repository = existing
		? await readApiData<GitRepository>(
				await page.request.put(`/api/customize/git-repositories/${existing.id}`, { data: body }),
				'Update GitOps test repository'
			)
		: await readApiData<GitRepository>(
				await page.request.post('/api/customize/git-repositories', { data: body }),
				'Create GitOps test repository'
			);

	await readApiData<{ message: string }>(
		await page.request.post(
			`/api/customize/git-repositories/${repository.id}/test?branch=${encodeURIComponent(GITOPS_REPO_BRANCH)}`
		),
		'Test GitOps repository connection'
	);

	return repository;
}

async function listSyncs(page: Page): Promise<GitOpsSync[]> {
	const response = await page.request.get(
		`/api/environments/0/gitops-syncs?search=${encodeURIComponent(GITOPS_SYNC_NAME)}&start=0&limit=100`
	);
	return readApiData<GitOpsSync[]>(response, 'List GitOps syncs');
}

async function ensureSync(page: Page, repositoryId: string): Promise<GitOpsSync> {
	const existing = (await listSyncs(page)).find((sync) => sync.name === GITOPS_SYNC_NAME);
	const body = {
		name: GITOPS_SYNC_NAME,
		repositoryId,
		branch: GITOPS_REPO_BRANCH,
		composePath: GITOPS_COMPOSE_PATH,
		targetType: 'project',
		projectName: GITOPS_PROJECT_NAME,
		autoSync: false,
		syncDirectory: false,
		pullImageAfterSync: false,
		redeployAfterSync: false
	};

	return existing
		? readApiData<GitOpsSync>(
				await page.request.put(`/api/environments/0/gitops-syncs/${existing.id}`, {
					data: body
				}),
				'Update GitOps test sync'
			)
		: readApiData<GitOpsSync>(
				await page.request.post('/api/environments/0/gitops-syncs', { data: body }),
				'Create GitOps test sync'
			);
}

async function getSync(page: Page, syncId: string): Promise<GitOpsSync> {
	return readApiData<GitOpsSync>(
		await page.request.get(`/api/environments/0/gitops-syncs/${syncId}`),
		'Get GitOps test sync'
	);
}

async function getProjects(page: Page): Promise<GitOpsProject[]> {
	return readApiData<GitOpsProject[]>(
		await page.request.get('/api/environments/0/projects?start=0&limit=100'),
		'List projects after GitOps sync'
	);
}

setup('create and verify the GitOps project prerequisite', async ({ page }) => {
	setup.setTimeout(120_000);

	const repository = await ensureRepository(page);
	let sync = await ensureSync(page, repository.id);

	await readApiData<{ success: boolean; message: string }>(
		await page.request.post(`/api/environments/0/gitops-syncs/${sync.id}/sync`),
		'Run GitOps test sync'
	);

	await expect
		.poll(
			async () => {
				sync = await getSync(page, sync.id);
				return {
					status: sync.lastSyncStatus,
					hasProject: Boolean(sync.projectId),
					hasCommit: Boolean(sync.lastSyncCommit),
					error: sync.lastSyncError ?? null
				};
			},
			{
				message: 'Expected GitOps sync to bind a managed project and commit',
				timeout: 60_000,
				intervals: [500, 1_000, 2_000]
			}
		)
		.toEqual({ status: 'success', hasProject: true, hasCommit: true, error: null });

	const projects = await getProjects(page);
	const project = projects.find((candidate) => candidate.id === sync.projectId);

	expect(project, 'Expected the GitOps sync project to exist in the projects API').toBeDefined();
	expect(project!.name).toBe(GITOPS_PROJECT_NAME);
	expect(project!.gitOpsManagedBy).toBe(sync.id);

	const projectDetail = await readApiData<GitOpsProject>(
		await page.request.get(`/api/environments/0/projects/${project!.id}`),
		'Get GitOps managed project details'
	);
	expect(projectDetail.lastSyncCommit).toBe(sync.lastSyncCommit);

	await page.goto('/projects');
	await expect(page.getByRole('link', { name: GITOPS_PROJECT_NAME, exact: true })).toBeVisible();
});
