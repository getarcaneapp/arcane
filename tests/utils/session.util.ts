import type { BrowserContext, Page } from '@playwright/test';

export type SessionBootstrap = {
	user: { preferences?: Record<string, unknown> } & Record<string, unknown>;
	environments: { data: object[] } & Record<string, unknown>;
} & Record<string, unknown>;

type SessionBootstrapPatch = (bootstrap: SessionBootstrap) => SessionBootstrap;

const sessionBootstrapPatches = new WeakMap<Page | BrowserContext, SessionBootstrapPatch[]>();

export async function patchSessionBootstrap(
	target: Page | BrowserContext,
	patch: SessionBootstrapPatch
) {
	const patches = sessionBootstrapPatches.get(target);
	if (patches) {
		patches.push(patch);
		return;
	}
	sessionBootstrapPatches.set(target, [patch]);
	await target.route(/\/api\/auth\/bootstrap(?:\?.*)?$/, async (route) => {
		const response = await route.fetch();
		if (!response.ok()) {
			await route.fulfill({ response });
			return;
		}
		const body: { data: SessionBootstrap } = await response.json();
		await route.fulfill({
			response,
			json: {
				...body,
				data: (sessionBootstrapPatches.get(target) ?? []).reduce(
					(bootstrap, apply) => apply(bootstrap),
					body.data
				)
			}
		});
	});
}

export function withEnvironments(
	bootstrap: SessionBootstrap,
	environments: object[]
): SessionBootstrap {
	return {
		...bootstrap,
		environments: {
			...bootstrap.environments,
			data: environments,
			pagination: {
				totalPages: 1,
				totalItems: environments.length,
				currentPage: 1,
				itemsPerPage: environments.length,
				grandTotalItems: environments.length
			}
		}
	};
}
