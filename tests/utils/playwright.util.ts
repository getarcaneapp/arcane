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

// Accepts either a `.cm-editor` root or its `.cm-content` element.
function codeMirrorContent(editor: Locator) {
	return editor
		.locator('.cm-content')
		.first()
		.or(editor.and(editor.page().locator('.cm-content')))
		.first();
}

function normalizeEditorText(text: string) {
	return text.replace(/\r/g, '').trimEnd();
}

export async function getCodeMirrorValue(editor: Locator) {
	const content = codeMirrorContent(editor);
	await expect(content).toBeVisible();
	return content.evaluate((node) => {
		const lineNodes = Array.from(node.querySelectorAll('.cm-line'));
		if (lineNodes.length > 0) {
			return lineNodes.map((line) => line.textContent ?? '').join('\n');
		}
		return node.textContent ?? '';
	});
}

// Replaces the editor document. Select-all can race the editor's focus and
// lint re-renders, so the clear step is verified and retried before typing.
export async function setCodeMirrorValue(editor: Locator, text: string) {
	const content = codeMirrorContent(editor);
	await expect(content).toBeVisible();
	await expect(content).not.toHaveAttribute('aria-readonly', 'true');
	await expect(async () => {
		await content.click({ position: { x: 10, y: 10 } });
		await expect(content).toBeFocused();
		await content.press('ControlOrMeta+A');
		await content.press('Backspace');
		expect(await getCodeMirrorValue(editor)).toBe('');
	}).toPass({ timeout: 10_000 });
	if (text.length > 0) {
		await content.page().keyboard.insertText(text);
	}
	await expect
		.poll(async () => normalizeEditorText(await getCodeMirrorValue(editor)), {
			message: 'Editor content must match the inserted text'
		})
		.toBe(normalizeEditorText(text));
}
