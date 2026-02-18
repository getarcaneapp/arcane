import { test, expect, type Page } from "@playwright/test";

const REFRESH_TOKEN_KEY = "arcane_refresh_token";
const TOKEN_EXPIRY_KEY = "arcane_token_expiry";

/**
 * Register an addInitScript that plants a fake refresh token in sessionStorage
 * BEFORE any page JavaScript runs on every navigation. Unlike page.evaluate(),
 * addInitScript survives page.goto() calls made later in the test.
 */
async function registerTokenSeeding(page: Page) {
  await page.addInitScript(
    ([tokenKey, expiryKey]: [string, string]) => {
      sessionStorage.setItem(tokenKey, "playwright-test-refresh-token");
      sessionStorage.setItem(expiryKey, new Date(Date.now() + 3_600_000).toISOString());
    },
    [REFRESH_TOKEN_KEY, TOKEN_EXPIRY_KEY],
  );
}

/**
 * Intercept the FIRST request matching urlPattern with a 401 version-mismatch
 * body. All subsequent requests (e.g. the interceptor's automatic retry) pass
 * through. Uses a flag rather than unroute() to avoid "Route is already handled".
 */
async function injectVersionMismatch401Once(page: Page, urlPattern: string | RegExp) {
  let fired = false;
  await page.route(urlPattern, async (route) => {
    if (!fired) {
      fired = true;
      await route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({
          code: "UNAUTHORIZED",
          message: "Application has been updated. Please log in again.",
        }),
      });
    } else {
      await route.continue();
    }
  });
}

/**
 * Intercept every request matching urlPattern with a 401.
 */
async function injectExpired401Always(page: Page, urlPattern: string | RegExp) {
  await page.route(urlPattern, async (route) => {
    await route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ code: "UNAUTHORIZED", message: "Invalid or expired token" }),
    });
  });
}

/**
 * Mock /auth/refresh to return a synthetic 200. Returns a getter to assert
 * whether the endpoint was called.
 */
async function mockRefreshSuccess(page: Page): Promise<() => boolean> {
  let called = false;
  await page.route(/\/api\/auth\/refresh$/, async (route) => {
    called = true;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        success: true,
        data: {
          token: "mocked-access-token",
          refreshToken: "mocked-refresh-token",
          expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        },
      }),
    });
  });
  return () => called;
}

test.describe("Token refresh behaviour", () => {
  test("version mismatch 401 on /auth/me during page load is silently recovered", async ({ page }) => {
    await registerTokenSeeding(page);
    const wasRefreshCalled = await mockRefreshSuccess(page);
    await injectVersionMismatch401Once(page, /\/api\/auth\/me$/);

    await page.goto("/dashboard");
    await page.waitForLoadState("networkidle");

    expect(wasRefreshCalled()).toBe(true);
    await expect(page).toHaveURL("/dashboard");
    await expect(page.getByRole("button", { name: "Sign in to Arcane" })).not.toBeVisible();
  });

  test("version mismatch 401 on a data endpoint mid-session is silently recovered", async ({ page }) => {
    await registerTokenSeeding(page);
    const wasRefreshCalled = await mockRefreshSuccess(page);
    await injectVersionMismatch401Once(page, /\/api\/environments\/0\/containers/);

    await page.goto("/containers");
    await page.waitForLoadState("networkidle");

    expect(wasRefreshCalled()).toBe(true);
    await expect(page).toHaveURL("/containers");
    await expect(page.getByRole("heading", { name: "Containers", level: 1 })).toBeVisible();
    await expect(page.getByRole("button", { name: "Sign in to Arcane" })).not.toBeVisible();
  });

  test("failed token refresh redirects to /login", async ({ page }) => {
    await registerTokenSeeding(page);

    await page.route(/\/api\/auth\/refresh$/, async (route) => {
      await route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: "UNAUTHORIZED", message: "Invalid or expired refresh token" }),
      });
    });

    // Make /auth/me always 401 so the interceptor triggers the refresh flow on load.
    await injectExpired401Always(page, /\/api\/auth\/me$/);

    await page.goto("/dashboard");
    await page.waitForURL(/\/login/, { timeout: 10_000 });
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("button", { name: "Sign in to Arcane", exact: true })).toBeVisible();
  });

  test("unauthenticated users are redirected to /login", async ({ page }) => {
    await page.context().clearCookies();
    await page.goto("/dashboard");
    await page.waitForURL(/\/login/, { timeout: 10_000 });
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("button", { name: "Sign in to Arcane", exact: true })).toBeVisible();
  });

  test("login page honours the redirect param and returns users to their original path", async ({ page }) => {
    await page.goto("/login?redirect=%2Fcontainers");
    await page.waitForURL(/\/containers|\/login/, { timeout: 8_000 });
    const url = page.url();
    expect(url).toMatch(/\/containers|\/login\?redirect=%2Fcontainers/);
  });
});
