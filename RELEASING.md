# Releasing

This repo ships two independently versioned artifacts:

- the **Go module** `github.com/sudarkoff/flagpole` — versioned by `vX.Y.Z` git tags.
- the **npm package** `@sudarkoff/flagpole-react` (in `react/`) — versioned in
  `react/package.json`, published to npm, tagged `react-vX.Y.Z`.

The two tag namespaces (`v*` vs `react-v*`) are deliberately distinct so releasing
one never triggers the other.

---

## Go module

Go modules have no upload step — a pushed semver tag *is* the release (the module
proxy fetches it on demand). There is no publish workflow.

1. Make sure `main` is green.
2. Pick the next semver: `vMAJOR.MINOR.PATCH`.
3. Tag and push:
   ```bash
   git checkout main && git pull
   git tag v0.2.0
   git push origin v0.2.0
   ```
4. (Optional) warm/verify the proxy:
   ```bash
   GOPROXY=proxy.golang.org go list -m github.com/sudarkoff/flagpole@v0.2.0
   ```

Consumers then `go get github.com/sudarkoff/flagpole@v0.2.0`.

> A `v2`+ breaking release requires the `/v2` module-path suffix
> (`module github.com/sudarkoff/flagpole/v2` in `go.mod`, and updated import
> paths). Not relevant until then.

---

## npm package (`@sudarkoff/flagpole-react`)

Publishing is automated by [`.github/workflows/publish-react.yml`](.github/workflows/publish-react.yml),
which triggers on `react-v*` tags and publishes to npm with **provenance** via
**OIDC trusted publishing** — no stored npm token.

### One-time setup (per package, on npmjs.com)

Required before the workflow can publish:

1. npmjs.com → **`@sudarkoff/flagpole-react`** → **Settings → Trusted Publisher**.
2. Add a **GitHub Actions** publisher:
   - Repository: `sudarkoff/flagpole`
   - Workflow filename: `publish-react.yml`
3. Save.

After this, the workflow authenticates via GitHub's OIDC token — there is no
`NODE_AUTH_TOKEN` secret to create or rotate.

> The very first publish of a brand-new package name sometimes has to be done
> manually (see "Manual fallback") because some registries require the package to
> exist before a trusted publisher can be attached. `@sudarkoff/flagpole-react`
> already exists, so trusted publishing applies from here on.

### Cutting a release

`react/.npmrc` sets `tag-version-prefix=react-v`, so `npm version` creates the
correctly-prefixed tag automatically.

1. Make sure `main` is green and your working tree is clean.
2. Bump + tag — run inside `react/`:
   ```bash
   cd react
   npm version patch        # or: minor | major
   ```
   This updates `react/package.json` + `react/package-lock.json`, makes a commit,
   and creates the tag `react-vX.Y.Z`.
3. Push the commit and the tag:
   ```bash
   git push origin main --follow-tags
   ```
4. The **publish-react** workflow runs: `typecheck` → `test` → `build` → verify the
   tag matches `package.json` version → `npm publish --provenance --access public`.

Watch the **Actions** tab; the new version appears at
<https://www.npmjs.com/package/@sudarkoff/flagpole-react>.

### Manual fallback

If you ever need to publish by hand (e.g. CI is down):

```bash
cd react
npm ci && npm run build
npm publish --access public      # prompts npm login; drop --provenance when local
```

Requires `npm login` and npm ≥ 11.5.1. A local publish cannot attach provenance
(that needs the CI OIDC token), so prefer the tag-triggered workflow.

---

## Versioning both at once

The packages version independently; bump only what changed. If a single change
touches both (e.g. a Go evaluator fix that the React docs reference), cut two
releases — `vX.Y.Z` for Go, `react-vX.Y.Z` for npm — with their own tags.
