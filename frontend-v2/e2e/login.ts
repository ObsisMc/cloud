import { expect, type Page } from '@playwright/test'

/** The owner from `v2-server/users.json`, the account the startup preset guarantees. */
export const ownerEmail = 'owner@demo.ora-space.dev'
/** A preset member, useful for the no-invitation perspective. */
export const memberEmail = 'alice@demo.ora-space.dev'

/**
 * Signs in through the real login screen. Every test gets a private browser context, so this is
 * the only way a spec becomes authenticated; it waits for the shell that only a session unlocks,
 * not for a particular account's name.
 */
export async function signIn(page: Page, email = ownerEmail) {
  await page.getByLabel('邮箱').fill(email)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '退出登录', exact: true }),
  ).toBeVisible()
}
