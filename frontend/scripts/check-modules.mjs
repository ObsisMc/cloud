#!/usr/bin/env node
// Module hygiene gate.
//
//   node scripts/check-modules.mjs               structural check of the tree
//   node scripts/check-modules.mjs --base <ref>  additionally, every module whose
//                                                implementation changed since the
//                                                merge base with <ref> (committed
//                                                or not) must also change its
//                                                READMEs and a test
//
// A "module" is any directory under ROOTS that directly contains hand-written
// source. Each module must carry README.md (中文) and README.en.md (English)
// describing what it does, and each module with implementation files must
// contain at least one *.test.ts(x).
//
// The diff check can be waived per pull request with a commit trailer:
//   Docs-Unchanged: <reason>     waives the README requirement
//   Tests-Unchanged: <reason>    waives the test requirement
// The reason is mandatory and is printed so reviewers see it.
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import {
  ROOTS,
  groupByDirectory,
  isSourceFile,
  isTestFile,
  isUnitSource,
  listFiles,
} from './module-files.mjs'

const README_FILES = ['README.md', 'README.en.md']
const MIN_README_CHARS = 80

function readmeProblem(dir, name) {
  const path = `${dir}/${name}`
  if (!existsSync(path)) return `${dir}: missing ${name}`
  const text = readFileSync(path, 'utf8')
  if (!/^#\s+\S/m.test(text)) return `${path}: needs a top-level '# ' heading`
  if (text.replace(/\s+/g, '').length < MIN_README_CHARS) {
    return `${path}: too short to describe the module (< ${MIN_README_CHARS} chars)`
  }
  return null
}

/** Returns modules as Map<dir, files[]> for every root that exists. */
function modules() {
  const files = ROOTS.filter((root) => existsSync(root)).flatMap((root) => listFiles(root))
  const groups = groupByDirectory(files)
  for (const [dir, group] of groups) {
    if (!group.some(isSourceFile)) groups.delete(dir)
  }
  return groups
}

function checkStructure(groups) {
  const problems = []
  for (const [dir, files] of groups) {
    for (const name of README_FILES) {
      const problem = readmeProblem(dir, name)
      if (problem) problems.push(problem)
    }
    if (files.some(isUnitSource) && !files.some(isTestFile)) {
      problems.push(`${dir}: has implementation files but no *.test.ts(x)`)
    }
  }
  return problems
}

function git(...args) {
  return execFileSync('git', args, { encoding: 'utf8' })
}

function waivers(mergeBase) {
  const log = git('log', '--format=%B', `${mergeBase}..HEAD`)
  const found = {}
  for (const key of ['Docs-Unchanged', 'Tests-Unchanged']) {
    const match = log.match(new RegExp(`^${key}:[ \\t]*(\\S.*)$`, 'm'))
    if (match) found[key] = match[1].trim()
  }
  return found
}

function checkDiff(groups, base) {
  // Diff the working tree against the merge base so the check is useful before
  // committing locally and matches what a pull request would merge in CI.
  const mergeBase = git('merge-base', base, 'HEAD').trim()
  const changed = new Set(
    [
      git('diff', '--name-only', '--relative', '--diff-filter=ACMRD', mergeBase),
      git('ls-files', '--others', '--exclude-standard'), // new files not yet added
    ]
      .join('\n')
      .split('\n')
      .filter(Boolean),
  )
  const waived = waivers(mergeBase)
  for (const [key, reason] of Object.entries(waived)) {
    console.warn(`${key} waiver in effect: ${reason}`)
  }
  const problems = []
  for (const [dir, files] of groups) {
    const touchedUnits = files.filter((file) => isUnitSource(file) && changed.has(file))
    if (touchedUnits.length === 0) continue
    const missingReadmes = README_FILES.filter((name) => !changed.has(`${dir}/${name}`))
    if (missingReadmes.length > 0 && !waived['Docs-Unchanged']) {
      problems.push(
        `${dir}: ${touchedUnits.join(', ')} changed but ${missingReadmes.join(' and ')} did not ` +
          '(add a "Docs-Unchanged: <reason>" commit trailer if the docs are truly unaffected)',
      )
    }
    if (
      !files.some((file) => isTestFile(file) && changed.has(file)) &&
      !waived['Tests-Unchanged']
    ) {
      problems.push(
        `${dir}: ${touchedUnits.join(', ')} changed but no test in the module did ` +
          '(add a "Tests-Unchanged: <reason>" commit trailer if behavior is unchanged)',
      )
    }
  }
  return problems
}

const baseIndex = process.argv.indexOf('--base')
const base = baseIndex === -1 ? null : process.argv[baseIndex + 1]
if (baseIndex !== -1 && !base) {
  console.error('--base requires a git ref')
  process.exit(2)
}

const groups = modules()
const problems = [...checkStructure(groups), ...(base ? checkDiff(groups, base) : [])]
if (problems.length > 0) {
  console.error('Module hygiene check failed:')
  for (const problem of problems) console.error(`  - ${problem}`)
  process.exit(1)
}
console.log(`Module hygiene OK: ${groups.size} modules${base ? ` (diff vs ${base})` : ''}`)
