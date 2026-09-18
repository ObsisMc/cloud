#!/usr/bin/env node
// Every exported declaration in hand-written source must carry a JSDoc block
// whose description says something the name does not already say.
//
// This is the mechanical half of the documentation rule in AGENTS.md: the lint
// (jsdoc/*) validates the shape of a comment that exists; this script makes
// sure the comment exists on the public surface at all.
import { existsSync, readFileSync } from 'node:fs'
import ts from 'typescript'
import { ROOTS, isUnitSource, listFiles } from './module-files.mjs'

const MIN_DESCRIPTION_WORDS = 3

const normalize = (text) =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim()
const splitCamel = (name) => name.replace(/([a-z])([A-Z])/g, '$1 $2')

/** Words in the description that are not just the identifier restated. */
function informativeWords(text, name) {
  const nameWords = new Set(normalize(splitCamel(name)).split(' '))
  return normalize(text)
    .split(' ')
    .filter((word) => word && !nameWords.has(word))
}

/** Names introduced by a top-level statement (several for `const a = 1, b = 2`). */
function declaredNames(node) {
  if (ts.isVariableStatement(node)) {
    return node.declarationList.declarations.map((d) => d.name.getText())
  }
  const named = 'name' in node && node.name && ts.isIdentifier(node.name)
  return named ? [node.name.getText()] : ['default']
}

function hasExportModifier(node) {
  const modifiers = ts.canHaveModifiers(node) ? ts.getModifiers(node) : undefined
  return modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword) ?? false
}

/** Top-level declarations by name, so `export { x }` lists can be traced back. */
function declarations(source) {
  const byName = new Map()
  for (const node of source.statements) {
    for (const name of declaredNames(node)) byName.set(name, node)
  }
  return byName
}

/**
 * Symbols exposed by an `export { ... }` statement. A local list points at the
 * declarations it names; a re-export from another module documents the
 * statement itself, since that is the only place a comment can live.
 */
function listedExports(node, byName) {
  if (!node.exportClause || !ts.isNamedExports(node.exportClause)) return []
  return node.exportClause.elements.map((element) => {
    const local = (element.propertyName ?? element.name).getText()
    const target = node.moduleSpecifier ? node : byName.get(local)
    return { name: element.name.getText(), node: target ?? node }
  })
}

/** `{ name, node }` for every exported symbol; `node` is the statement whose JSDoc documents it. */
function exportedSymbols(source) {
  const byName = declarations(source)
  return source.statements.flatMap((node) => {
    if (ts.isExportDeclaration(node)) return listedExports(node, byName)
    if (!hasExportModifier(node)) return []
    return declaredNames(node).map((name) => ({ name, node }))
  })
}

/** Concatenated JSDoc description text, or null when no JSDoc block exists. */
function description(node) {
  const docs = ts.getJSDocCommentsAndTags(node).filter(ts.isJSDoc)
  if (docs.length === 0) return null
  return docs
    .map((doc) =>
      typeof doc.comment === 'string' ? doc.comment : ts.getTextOfJSDocComment(doc.comment),
    )
    .filter(Boolean)
    .join(' ')
}

function problemFor(file, source, { name, node }) {
  const line = source.getLineAndCharacterOfPosition(node.getStart()).line + 1
  const text = description(node)
  if (text === null) return `${file}:${line}: exported ${name} has no JSDoc`
  if (informativeWords(text, name).length < MIN_DESCRIPTION_WORDS) {
    return `${file}:${line}: JSDoc for ${name} only restates the name; describe purpose or invariants`
  }
  return null
}

function checkFile(file) {
  const source = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true)
  return exportedSymbols(source)
    .map((symbol) => problemFor(file, source, symbol))
    .filter(Boolean)
}

const files = ROOTS.filter((root) => existsSync(root))
  .flatMap((root) => listFiles(root))
  .filter(isUnitSource)
const problems = files.flatMap(checkFile)
if (problems.length > 0) {
  console.error('Undocumented exports:')
  for (const problem of problems) console.error(`  - ${problem}`)
  process.exit(1)
}
console.log(`Exports documented OK: ${files.length} files`)
