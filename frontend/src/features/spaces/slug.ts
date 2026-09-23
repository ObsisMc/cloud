/**
 * Slug rules shared by every place a space is named. The pattern mirrors the
 * backend check exactly; the derivation only proposes a default the user may
 * still edit.
 */

/** Backend rule for space slugs: lowercase, digits and hyphens, 1–64 chars, no leading hyphen. */
export const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/

/** True when `slug` would be accepted by the backend as-is. */
export function isValidSlug(slug: string): boolean {
  return SLUG_PATTERN.test(slug)
}

/**
 * Proposes a slug from a display name: lowercase ASCII letters and digits,
 * runs of anything else collapsed into single hyphens, trimmed to the
 * backend's length. Names with no ASCII content yield `''`, which the form
 * treats as "type one yourself".
 */
export function slugFromName(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
}
