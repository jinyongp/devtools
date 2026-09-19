# GitHub Pages installer endpoint

## Goal

Serve the latest stable devtools release installer from the devtools repository's own GitHub Pages project site so the public install command becomes:

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
```

The `jinyongp.github.io` repository remains untouched.

## Design

- Use the `devtools` project Pages site. The user-site custom domain `jinyongp.dev` is inherited, yielding the `/devtools` project path.
- Keep `scripts/install.sh` as the single installer source.
- Allow omitted installer action to mean `install`.
- Publish the exact `install.sh` asset from a successful stable GitHub Release to Pages.
- Run Pages deployment as a separate `workflow_run` from the default branch after the Release workflow succeeds, preserving the `github-pages` environment's default-branch protection.
- Keep manual dispatch so the latest stable release can be seeded or recovered independently.
- Deploy only `install.sh` and `.nojekyll` for now; the project Pages namespace remains available for future docs.
- Do not assign a separate custom domain to the devtools repository.

## Validation

- installer default-action regression
- documented one-line bootstrap regression
- actionlint for Release + Pages workflows
- exact release asset/source comparison in Pages workflow
- full relevant tests / diff check
- `workflow_run` must use the default branch ref so the `github-pages` environment can remain restricted to the default branch

## Release follow-up — v0.17.0

- v0.17.0 passed macOS/Linux/Skill discovery/Chromium/GitHub Release/Homebrew.
- Direct reusable Pages invocation from the tag was rejected by the `github-pages` environment because `GITHUB_REF` was `v0.17.0`.
- Pages deployment is moved to `workflow_run`, whose `GITHUB_REF` is the default branch, while the published release tag is carried in `workflow_run.head_branch`.

## Release annotation cleanup

- Run repository-owned Linux jobs on `ubuntu-24.04` instead of the moving `ubuntu-latest` label.
- Disable `setup-uv` cache persistence because devtools has no Python dependency manifest; uv is only needed for the Agent Skills validator.
- Keep Pages deployment on the default-branch `workflow_run` so the `github-pages` environment protection rule stays strict.
