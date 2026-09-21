import { expect, type Locator } from '@playwright/test';
import playwrightConfig from '../playwright.config';

export async function createTestApiKeys(count: number = 2) {
	const url = new URL('/api/playwright/create-test-api-keys', playwrightConfig.use!.baseURL);

	const response = await fetch(url, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ count })
	});

	if (!response.ok) {
		throw new Error(`Failed to create test API keys: ${response.status} ${response.statusText}`);
	}

	return response.json();
}

export async function deleteTestApiKeys() {
	const url = new URL('/api/playwright/delete-test-api-keys', playwrightConfig.use!.baseURL);

	const response = await fetch(url, {
		method: 'POST'
	});

	if (!response.ok) {
		throw new Error(`Failed to delete test API keys: ${response.status} ${response.statusText}`);
	}
}

export async function waitForDialogReady(dialog: Locator) {
	await expect(dialog).toBeVisible();
	await dialog.evaluate(async (element) => {
		await Promise.all(element.getAnimations().map((animation) => animation.finished));
	});
	await expect
		.poll(
			() => dialog.evaluate((element) => element.contains(element.ownerDocument.activeElement)),
			{ message: 'Dialog must finish opening and receive focus before input' }
		)
		.toBe(true);
}
