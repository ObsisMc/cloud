# scripts: repository gate scripts

[中文](README.md) | [English](README.en.md)

## Responsibility

Turns the rules in `AGENTS.md` that no off-the-shelf lint can express into machine checks. Plain Node ESM with no extra dependencies (only `typescript` for parsing), invoked through the `check:*` scripts in `package.json`; CI and `task frontend:check` use the same entry points.

## Contents

| File | Description |
| --- | --- |
| `module-files.mjs` | Shared file-classification policy: what counts as a module, an implementation file, or generated output. Used by both checks so the definitions cannot drift. |
| `check-modules.mjs` | Module hygiene: every directory with source needs `README.md` + `README.en.md`, every directory with implementation files needs a test; with `--base <ref>`, modules whose implementation changed must also change their READMEs and a test, waivable with a reasoned `Docs-Unchanged:` / `Tests-Unchanged:` commit trailer. |
| `check-exports-documented.mjs` | Every exported symbol needs a JSDoc whose description says more than the name. |

## Conventions

- Changes to these scripts update this README too; behavior changes show before/after output in the PR description.
- No business logic or build steps live here; Vite owns the build.
