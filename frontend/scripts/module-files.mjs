// Shared file-classification policy for the hygiene checks. Keeping it in one
// place means "what counts as a module" cannot drift between the structural
// check and the diff check.
import { readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

/** Directories whose contents are generated or vendored and never hand-maintained. */
export const GENERATED_DIRS = ['src/api']

/** Roots scanned for modules; every directory below them is a candidate. */
export const ROOTS = ['src', 'scripts']

const SOURCE_EXTENSIONS = ['.ts', '.tsx', '.mjs', '.css']
const UNIT_EXTENSIONS = ['.ts', '.tsx']

/** Normalizes a path to forward slashes relative to the frontend root. */
export function toPosix(path) {
  return relative(process.cwd(), path).split(sep).join('/')
}

export function isGenerated(path) {
  const posix = toPosix(path)
  return GENERATED_DIRS.some((dir) => posix === dir || posix.startsWith(`${dir}/`))
}

export function isTestFile(path) {
  return /\.test\.tsx?$/.test(path)
}

/** Hand-written implementation file: a unit that needs docs and tests. */
export function isUnitSource(path) {
  return (
    UNIT_EXTENSIONS.some((ext) => path.endsWith(ext)) &&
    !isTestFile(path) &&
    !path.endsWith('.d.ts') &&
    !isGenerated(path)
  )
}

/** Any hand-written file that makes a directory a module needing a README. */
export function isSourceFile(path) {
  return SOURCE_EXTENSIONS.some((ext) => path.endsWith(ext)) && !isGenerated(path)
}

/** Recursively lists files under `dir` as paths relative to the frontend root. */
export function listFiles(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) {
      if (!isGenerated(full)) out.push(...listFiles(full))
    } else {
      out.push(toPosix(full))
    }
  }
  return out
}

/** Groups files by their containing directory (posix, relative). */
export function groupByDirectory(files) {
  const groups = new Map()
  for (const file of files) {
    const dir = file.includes('/') ? file.slice(0, file.lastIndexOf('/')) : '.'
    const group = groups.get(dir) ?? []
    group.push(file)
    groups.set(dir, group)
  }
  return groups
}
