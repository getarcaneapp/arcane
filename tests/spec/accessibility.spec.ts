import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '../fixtures/test.fixture';
import authUtil from '../utils/auth.util';

type AxeViolation = Awaited<ReturnType<AxeBuilder['analyze']>>['violations'][number];

function formatViolations(violations: AxeViolation[]): string {
	return violations
		.map((violation) => {
			const targets = violation.nodes.map((node) => node.target.join(' > ')).join(', ');
			return `${violation.id} (${violation.impact ?? 'unknown'}): ${targets}`;
		})
		.join('\n');
}

async function expectNoSeriousAccessibilityViolations(page: Page, label: string, include?: string) {
	let builder = new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
		.disableRules(['color-contrast', 'html-lang-valid']);
	if (include) {
		await expect(page.locator(include)).toBeVisible();
		builder = builder.include(include);
	}

	const results = await builder.analyze();
	const violations = results.violations.filter(
		(violation) => violation.impact === 'critical' || violation.impact === 'serious'
	);

	expect(violations, `${label}\n${formatViolations(violations)}`).toEqual([]);
}

test('login and representative authenticated surfaces pass the accessibility checks', async ({
	page
}) => {
	test.setTimeout(120_000);

	await page.goto('/login');
	await expect(page).toHaveTitle(/Arcane/);
	await expectNoSeriousAccessibilityViolations(page, 'Login page');

	await page.getByLabel('Username').fill('arcane');
	await page.getByLabel('Password').fill(authUtil.TEST_PASSWORD);
	await page.getByRole('button', { name: 'Sign in to Arcane', exact: true }).click();
	await expect(page).toHaveURL('/dashboard');
	await expect(page.getByRole('button', { name: 'Card view', exact: true })).toBeVisible();
	await expectNoSeriousAccessibilityViolations(page, 'Dashboard', 'main');

	await page.goto('/networks');
	await expect(page.getByRole('heading', { name: 'Networks', level: 1 })).toBeVisible();
	await expectNoSeriousAccessibilityViolations(page, 'Networks table', 'main');

	await page.goto('/containers/new');
	await expect(page.getByRole('heading', { name: 'Create Container' })).toBeVisible();
	await expectNoSeriousAccessibilityViolations(page, 'New container form', 'main');

	await page.goto('/networks');
	await expect(page.getByRole('heading', { name: 'Networks', level: 1 })).toBeVisible();
	await page.getByRole('button', { name: 'Create Network' }).first().click();
	await expect(page.getByRole('dialog')).toBeVisible();
	await expectNoSeriousAccessibilityViolations(page, 'Create network dialog', '[role="dialog"]');
});
