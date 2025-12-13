import { test, expect, type Page } from '@playwright/test';

const ROUTES = {
  page: '/events',
  apiCreate: '/api/events',
};

async function navigateToEvents(page: Page) {
  await page.goto(ROUTES.page);
  await page.waitForLoadState('networkidle');
}

async function createTestEvent(page: Page, environmentId: string, title: string) {
  const res = await page.request.post(ROUTES.apiCreate, {
    data: {
      type: 'system.prune',
      severity: 'info',
      title,
      description: 'playwright test event',
      environmentId,
      metadata: { playwright: true },
    },
  });

  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  const id = body?.data?.id as string | undefined;
  expect(id).toBeTruthy();
  return id!;
}

async function deleteTestEvent(page: Page, id: string) {
  const res = await page.request.delete(`/api/events/${id}`);
  // Best-effort cleanup (events can race in parallel runs)
  expect([200, 404]).toContain(res.status());
}

test.describe('Events Page', () => {
  test('should scope the event list to the selected environment (default env 0)', async ({ page }) => {
    const titleEnv0 = `pw-env0-${Date.now()}`;
    const titleOther = `pw-other-${Date.now()}`;

    const idEnv0 = await createTestEvent(page, '0', titleEnv0);
    const idOther = await createTestEvent(page, '999', titleOther);

    try {
      await navigateToEvents(page);

      // Note: the desktop Events table does not show the title column; the title is shown
      // in the details dialog and in mobile cards. So we search for the event, then open
      // its details to assert it's present in the scoped list.
      const search = page.getByPlaceholder(/search/i);

      // Search for the env0 event
      await search.fill(titleEnv0);
      await search.press('Enter');

      // Open row actions -> View details
      await page.getByRole('button', { name: 'Open menu' }).first().click();
      await page.getByRole('menuitem', { name: /view details/i }).click();

      // Title should be visible inside the details dialog
      await expect(page.getByRole('heading', { name: titleEnv0 })).toBeVisible();

      // Close dialog so the next search isn't obstructed
      await page.keyboard.press('Escape');
      await expect(page.getByRole('heading', { name: titleEnv0 })).toHaveCount(0);

      // Search for the other-environment event, which should NOT exist in this environment-scoped list.
      await search.fill(titleOther);
      await search.press('Enter');

      // No rows should be present for that search.
      await expect(page.getByRole('button', { name: 'Open menu' })).toHaveCount(0);
    } finally {
      await deleteTestEvent(page, idEnv0);
      await deleteTestEvent(page, idOther);
    }
  });
});
