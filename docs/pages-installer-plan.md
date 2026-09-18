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
- Implement Pages publishing as a reusable/manual workflow:
  - stable Release workflow calls it after the release job succeeds;
  - manual dispatch can seed the current latest stable release once Pages is enabled.
- Deploy only `install.sh` and `.nojekyll` for now; the project Pages namespace remains available for future docs.
- Do not assign a separate custom domain to the devtools repository.

## Validation

- installer default-action regression
- documented one-line bootstrap regression
- actionlint for Release + Pages workflows
- exact release asset/source comparison in Pages workflow
- full relevant tests / diff check
- remote Pages deployment requires the repository's one-time Settings > Pages > Source = GitHub Actions setting
