import { test as setup } from '@playwright/test';
import authUtil from '../utils/auth.util';

const authFile = '.auth/login.json';

setup('authenticate', async ({ page }) => {
	const currentPassword = await authUtil.login(page);

	await page.waitForURL('/dashboard');

	await authUtil.changeDefaultPassword(page, currentPassword, authUtil.TEST_PASSWORD);

	await page.context().storageState({ path: authFile });
});
