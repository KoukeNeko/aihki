# Taiga CLI product website

A custom Svelte 5 + TypeScript + Vite static product page. Visual direction follows the supplied Hero image: graph paper, mint/teal, navy typography, soft paper cards and subtle rotated layers. English product copy targets the international developer audience. Product claims and executable examples are based on the repository README.

The interactive terminal examples now use output captured from the installed Taiga CLI v0.6.0 against a disposable local Taiga 6.10.2 environment. `public/cli-session.txt` contains the full redacted session. The temporary username is replaced with `<user>`, the token was never printed, and the local containers and volumes were removed after recording.

## Local development

The footer's tested Taiga server versions are read from `Verified` rows in the root `COMPATIBILITY.md` at build time. Update that document after verification, then rebuild and deploy Pages to update the website. This is not a live latest-version lookup. Recorded session versions remain historical. The Go and MIT labels describe implementation and licensing, rather than release versions.

```sh
cd website
npm ci
npm run dev -- --host 127.0.0.1
```

```sh
npm run check
npm test
npm run build
npm run preview -- --host 127.0.0.1
```

The build lives in `website/dist`. Relative asset URLs allow deployment at `/taiga-cli/` or a nested path. The page is a single document with anchor navigation, so GitHub Pages does not need SPA route rewrites. Fonts load from Google Fonts with local fallbacks. No analytics, backend, or live Taiga access is required. The copy buttons use the Clipboard API on HTTPS or localhost, with a live-region error message and selectable commands if access is unavailable.

## GitHub Pages: preserve the package repositories

The existing repository site is https://koukeneko.github.io/taiga-cli/ and already hosts signed APT/DNF repositories. GitHub Pages serves one deployment artifact per repository. **Do not publish `website/dist` as a standalone Pages artifact:** that would remove the package paths.

The existing `.github/workflows/package-repo.yml` remains the only Pages publisher. It now:

1. Rebuilds signed package repositories as before.
2. Installs, checks, tests and builds the Svelte site.
3. Merges website files into `site/`, replacing only the former root landing page.
4. Uploads the complete `site/` artifact and deploys it through the existing Pages environment.

`deb/`, `rpm/`, and `taiga-cli.gpg.key` retain their paths. The assembly script refuses missing package content, reserved website paths and collisions with other existing content. Regression tests check byte-for-byte preservation using temporary package fixtures; they do not constitute a live signed package deployment test.

No new push trigger is added. After merging these changes, manually run **Package repo** (or let the next successful stable Release trigger it). This requires the existing `APT_GPG_PRIVATE_KEY` secret and Pages environment. Product page changes alone do not automatically deploy. Local work does not change the published site. If any step fails, deployment is not reached and the existing published artifact remains in place.

This repository's Pages deployment does not replace another repository's user/project site. The root page of _this_ repository will intentionally change from package setup text to the product page. Its installation guide still links to the existing package setup instructions.

References: [GitHub Pages](https://docs.github.com/en/pages/getting-started-with-github-pages/what-is-github-pages), [Vite static deployment](https://vite.dev/guide/static-deploy).
