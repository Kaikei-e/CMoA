---
title: Adopt DocDag for architecture decision records
status: accepted
date: 2026-09-04
---

# Adopt DocDag for architecture decision records

## Context and Problem Statement

CMoA needs a place for architecture decisions that stays consistent as the
corpus grows: a superseded record whose status never moved, a cycle in the
supersession chain, or a `supersedes:` pointing at a file nobody wrote. Those
are graph properties. Review does not catch them reliably, and a directory of
Markdown files does not answer "what is in force" without reading every file.

## Decision Drivers

* Decisions must remain ordinary Markdown, so humans and agents edit the same
  files.
* The graph of those decisions must be checkable in CI in one command.
* An agent should ask the graph (`context`, `query --binding`, `resolve`)
  rather than fan out over the directory.

## Considered Options

* Keep decisions as unstructured Markdown with no gate.
* Adopt DocDag v0.3.0 with the `adr` preset.
* Adopt DocDag v0.3.0 with the `spec` preset (clauses, conformance tests,
  deviations, periods).

## Decision Outcome

Chosen option: **DocDag v0.3.0 with the `adr` preset**.

Decision records live in `docs/adr` as `NNNN-kebab-title.md`. Typed edges
are `supersedes` and `depends-on`, declared in frontmatter. `docdag.yaml`
pins `preset: adr`, `preset_version: 1`, and `dir: docs/adr`, and raises
`missing_frontmatter` and dangling identifier-shaped references to errors.

The `spec` preset is the wrong shape for this repository: CMoA is recording
decisions, not a normative standard of clauses and conformance tests.

CI runs `docdag validate` via `Kaikei-e/DocDag@v0.3.0`. A later decision
that replaces this one declares `supersedes: 0001` in its frontmatter and
sets this record's status to `superseded`.
