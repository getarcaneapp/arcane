import { expect, test, type Locator, type Page } from '../fixtures/test.fixture';
import { readApiData } from '../utils/fetch.util';
import { openRowActionsMenu } from '../utils/table-actions.util';

type GlobalVariable = {
	id: string;
	key: string;
	value: string;
	isSecret: boolean;
	allEnvironments: boolean;
	environmentIds: string[];
};

type VariableMutation = {
	variable?: GlobalVariable;
};

type Template = {
	id: string;
	name: string;
	description: string;
};

type Project = {
	id: string;
	name: string;
	status?: string;
	runtimeServices?: Array<{ containerId?: string; containerName?: string }>;
};

type ContainerDetails = {
	id: string;
	config: { env?: string[] };
};

async function setCodeMirrorValue(page: Page, editor: Locator, text: string) {
	const content = editor.locator('.cm-content').first();
	await expect(content).toBeVisible();
	await content.click({ position: { x: 10, y: 10 } });
	await content.press('ControlOrMeta+A');
	await page.keyboard.insertText(text);
}

async function listVariables(page: Page): Promise<GlobalVariable[]> {
	return readApiData<GlobalVariable[]>(
		await page.request.get('/api/variables'),
		'List global variables'
	);
}

function variableRow(page: Page, key: string) {
	return page.getByRole('row').filter({ has: page.getByText(key, { exact: true }) });
}

async function openVariableSheet(page: Page) {
	await page.getByRole('button', { name: 'Add Variable', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Create Variable' });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function createSingleVariable(
	page: Page,
	key: string,
	value: string,
	isSecret = false
): Promise<GlobalVariable> {
	const dialog = await openVariableSheet(page);
	await dialog.locator('#variable-key').fill(key);
	await dialog.locator('#variable-value').fill(value);
	if (isSecret) await dialog.locator('#variable-secret').click();

	const responsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' &&
			new URL(response.url()).pathname === '/api/variables'
	);
	await dialog.getByRole('button', { name: 'Add Variable', exact: true }).click();
	const result = await readApiData<VariableMutation>(
		await responsePromise,
		`Create global variable ${key}`
	);
	expect(result.variable).toBeDefined();
	await expect(dialog).toBeHidden();
	return result.variable!;
}

async function getProject(page: Page, projectId: string): Promise<Project> {
	return readApiData<Project>(
		await page.request.get(`/api/environments/0/projects/${projectId}`),
		`Get project ${projectId}`
	);
}

test('manages variables and deploys an edited template with real substitution', async ({
	page
}) => {
	test.setTimeout(240_000);
	page.setDefaultTimeout(15_000);

	const suffix = Date.now().toString(36).toUpperCase();
	const publicKey = `E2E_TEMPLATE_VALUE_${suffix}`;
	const publicValue = `resolved-${suffix.toLowerCase()}`;
	const secretKey = `E2E_TEMPLATE_SECRET_${suffix}`;
	const secretValue = `secret-${suffix.toLowerCase()}`;
	const bulkKeyOne = `E2E_BULK_ONE_${suffix}`;
	const bulkKeyTwo = `E2E_BULK_TWO_${suffix}`;
	const templateName = `E2E Variable Template ${suffix}`;
	const updatedTemplateName = `${templateName} Updated`;
	const projectName = `e2e-variable-project-${suffix.toLowerCase()}`;
	const containerName = `e2e-variable-container-${suffix.toLowerCase()}`;
	let templateId: string | null = null;
	let projectId: string | null = null;

	const composeContent = [
		'services:',
		'  verifier:',
		'    image: public.ecr.aws/docker/library/busybox:1.37',
		`    container_name: ${containerName}`,
		'    command:',
		'      - sh',
		'      - -c',
		'      - test "$' + publicKey + '" = "' + publicValue + '" && sleep 3600',
		'    environment:',
		`      ${publicKey}: \${${publicKey}}`,
		''
	].join('\n');

	try {
		const localEnvironment = await readApiData<{ id: string; name: string }>(
			await page.request.get('/api/environments/0'),
			'Get local environment for variable scope'
		);

		await page.goto('/customize/variables');
		const publicVariable = await createSingleVariable(page, publicKey, publicValue);
		expect(publicVariable).toMatchObject({
			key: publicKey,
			value: publicValue,
			isSecret: false,
			allEnvironments: true
		});
		await expect(variableRow(page, publicKey)).toContainText(publicValue);

		const secretVariable = await createSingleVariable(page, secretKey, secretValue, true);
		expect(secretVariable).toMatchObject({ key: secretKey, value: '', isSecret: true });
		const secretRow = variableRow(page, secretKey);
		await expect(secretRow).toContainText('••••••••');
		await expect(secretRow).not.toContainText(secretValue);

		const bulkDialog = await openVariableSheet(page);
		await bulkDialog.getByRole('button', { name: 'Paste .env', exact: true }).click();
		await bulkDialog
			.getByLabel('Paste .env content', { exact: true })
			.fill(`${bulkKeyOne}=first\n${bulkKeyTwo}=second`);
		await bulkDialog.getByRole('radio', { name: /Specific Environments/ }).click();
		const environmentOption = bulkDialog
			.locator('label')
			.filter({ hasText: localEnvironment.name });
		await environmentOption.getByRole('switch').click();
		await bulkDialog.getByRole('button', { name: 'Add Variable', exact: true }).click();
		await expect(bulkDialog).toBeHidden();
		await expect(variableRow(page, bulkKeyOne)).toBeVisible();
		await expect(variableRow(page, bulkKeyTwo)).toBeVisible();

		let variables = await listVariables(page);
		for (const key of [bulkKeyOne, bulkKeyTwo]) {
			const variable = variables.find((candidate) => candidate.key === key);
			expect(variable).toMatchObject({
				allEnvironments: false,
				environmentIds: ['0']
			});
		}

		const editableRow = variableRow(page, bulkKeyOne);
		const editMenu = await openRowActionsMenu(page, editableRow);
		await editMenu.getByRole('menuitem', { name: 'Edit', exact: true }).click();
		const editDialog = page.getByRole('dialog', { name: 'Edit Variable' });
		await editDialog.locator('#variable-value').fill('first-edited');
		const editResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'PUT' &&
				/^\/api\/variables\/[^/]+$/.test(new URL(response.url()).pathname)
		);
		await editDialog.getByRole('button', { name: 'Save Changes', exact: true }).click();
		expect((await editResponsePromise).ok()).toBe(true);
		await expect(editableRow).toContainText('first-edited');

		const secretMenu = await openRowActionsMenu(page, secretRow);
		await secretMenu.getByRole('menuitem', { name: 'Delete', exact: true }).click();
		const secretDeleteResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'DELETE' &&
				new URL(response.url()).pathname === `/api/variables/${secretVariable.id}`
		);
		await page.getByRole('dialog').getByRole('button', { name: 'Delete', exact: true }).click();
		expect((await secretDeleteResponsePromise).ok()).toBe(true);
		await expect(secretRow).toHaveCount(0);

		const syncResults = await readApiData<
			Array<{ environmentId: string; status: 'synced' | 'pending' | 'error'; error?: string }>
		>(await page.request.post('/api/variables/sync'), 'Synchronize global variables');
		expect(syncResults).toEqual(
			expect.arrayContaining([expect.objectContaining({ environmentId: '0', status: 'synced' })])
		);

		await page.goto('/customize/templates/create');
		await page.getByRole('button', { name: 'e.g. My Nginx Project', exact: true }).click();
		const templateNameInput = page
			.getByPlaceholder('e.g. My Nginx Project', { exact: true })
			.filter({ visible: true });
		await templateNameInput.fill(templateName);
		await templateNameInput.press('Enter');
		await page
			.getByLabel('Description', { exact: true })
			.fill('Created by the customization browser journey');

		const editors = page.locator('.cm-editor').filter({ visible: true });
		const composeEditor = editors.first();
		const envEditor = editors.nth(1);
		await setCodeMirrorValue(page, composeEditor, 'services:\n  broken: [\n');
		await setCodeMirrorValue(page, envEditor, 'NOT VALID ENV');
		await expect(page.getByRole('button', { name: 'Create Template', exact: true })).toBeDisabled();

		await setCodeMirrorValue(page, composeEditor, composeContent);
		await setCodeMirrorValue(page, envEditor, 'TEMPLATE_LOCAL=value\n');
		const createTemplateButton = page.getByRole('button', { name: 'Create Template', exact: true });
		await expect(createTemplateButton).toBeEnabled({ timeout: 20_000 });
		const templateCreateResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === '/api/templates'
		);
		await createTemplateButton.click();
		const template = await readApiData<Template>(
			await templateCreateResponsePromise,
			`Create template ${templateName}`
		);
		templateId = template.id;
		await expect(page).toHaveURL(`/customize/templates/${template.id}`);

		await page.getByRole('button', { name: templateName, exact: true }).click();
		const editTemplateNameInput = page
			.getByPlaceholder('e.g. My Nginx Project', { exact: true })
			.filter({ visible: true });
		await editTemplateNameInput.fill(updatedTemplateName);
		await editTemplateNameInput.press('Enter');
		await page.getByLabel('Description', { exact: true }).fill('Updated customization template');
		const templateUpdateResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'PUT' &&
				new URL(response.url()).pathname === `/api/templates/${template.id}`
		);
		await page.getByRole('button', { name: 'Save', exact: true }).click();
		const updatedTemplate = await readApiData<Template>(
			await templateUpdateResponsePromise,
			`Update template ${template.id}`
		);
		expect(updatedTemplate).toMatchObject({
			name: updatedTemplateName,
			description: 'Updated customization template'
		});

		await page.getByRole('button', { name: 'Open menu', exact: true }).click();
		await page.getByRole('menuitem', { name: 'Create Project', exact: true }).click();
		await expect(page).toHaveURL(`/projects/new?templateId=${template.id}`);
		const projectNameButton = page.getByRole('button', {
			name: updatedTemplateName
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, '-')
				.replace(/^-|-$/g, ''),
			exact: true
		});
		await projectNameButton.click();
		const projectNameInput = page
			.getByPlaceholder('My New Project', { exact: true })
			.filter({ visible: true });
		await projectNameInput.fill(projectName);
		await projectNameInput.press('Enter');
		await expect(page.locator('.cm-editor').filter({ visible: true }).first()).toContainText(
			publicKey
		);

		const createProjectButton = page.locator('button[data-action="create"]');
		await expect(createProjectButton).toBeEnabled({ timeout: 20_000 });
		const projectCreateResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === '/api/environments/0/projects'
		);
		await createProjectButton.click();
		const project = await readApiData<Project>(
			await projectCreateResponsePromise,
			`Create project from template ${template.id}`
		);
		projectId = project.id;
		await expect(page).toHaveURL((url) => url.pathname === `/projects/${project.id}`);

		const deployResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === `/api/environments/0/projects/${project.id}/up`,
			{ timeout: 30_000 }
		);
		await page
			.getByRole('button', { name: 'Up', exact: true })
			.filter({ visible: true })
			.first()
			.click();
		expect((await deployResponsePromise).ok()).toBe(true);
		await expect
			.poll(async () => (await getProject(page, project.id)).status, { timeout: 60_000 })
			.toBe('running');

		let containerId = '';
		await expect
			.poll(
				async () => {
					const runtime = await readApiData<Project>(
						await page.request.get(`/api/environments/0/projects/${project.id}/runtime`),
						`Get runtime services for ${project.id}`
					);
					containerId =
						runtime.runtimeServices?.find((service) => service.containerName === containerName)
							?.containerId ?? '';
					return containerId;
				},
				{ timeout: 30_000 }
			)
			.not.toBe('');
		const container = await readApiData<ContainerDetails>(
			await page.request.get(`/api/environments/0/containers/${containerId}`),
			`Inspect substituted container ${containerId}`
		);
		expect(container.config.env).toContain(`${publicKey}=${publicValue}`);

		await page.request.delete(`/api/environments/0/projects/${project.id}/destroy`, {
			data: { removeVolumes: false }
		});
		projectId = null;

		await page.goto(`/customize/templates/${template.id}`);
		await page.getByRole('button', { name: 'Open menu', exact: true }).click();
		await page.getByRole('menuitem', { name: 'Delete Template', exact: true }).click();
		const templateDeleteResponsePromise = page.waitForResponse(
			(response) =>
				response.request().method() === 'DELETE' &&
				new URL(response.url()).pathname === `/api/templates/${template.id}`
		);
		await page
			.getByRole('dialog')
			.getByRole('button', { name: 'Delete Template', exact: true })
			.click();
		expect((await templateDeleteResponsePromise).ok()).toBe(true);
		await expect(page).toHaveURL((url) => url.pathname === '/customize/templates');
		templateId = null;
	} finally {
		if (projectId) {
			await page.request
				.delete(`/api/environments/0/projects/${projectId}/destroy`, {
					data: { removeVolumes: false }
				})
				.catch(() => undefined);
		}
		if (templateId) {
			await page.request.delete(`/api/templates/${templateId}`).catch(() => undefined);
		}
		const variables = await listVariables(page).catch(() => []);
		for (const variable of variables.filter((candidate) => candidate.key.endsWith(suffix))) {
			await page.request.delete(`/api/variables/${variable.id}`).catch(() => undefined);
		}
	}
});
