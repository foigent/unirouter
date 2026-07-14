# V1 Post-PRD6 Docker Context Local Evidence — 2026-07-14

## Status and boundary

- Status: Implemented; refreshed Draft PR #195 exact-head required CI pending.
- Baseline: `origin/main` at
  `8cd268549917bb548e8b58a53c4a0f2b135a5781`.
- Classification: confirmed bug/regression; High / P1 release security and
  build-cache hygiene.
- PRD6 remains `Completed (Accepted Scope)`. This does not reopen PRD6 and no
  image, ECS service, credential, target database or target Redis state was
  changed.

## Broken control and reachability

1. The repository had a local root `.env.production`, while the backend
   Dockerfile uses `COPY . .`. Without a context policy, local environment
   material can reach the builder filesystem and reusable layer cache even
   though the final runtime stage copies only the binary. Secret contents were
   never printed or recorded.
2. Compose builds the backend with context `.` but builds the frontend with
   context `./frontend`. Docker resolves `.dockerignore` from each context
   root, so a repository-root policy cannot protect an independent frontend
   build context.
3. The frontend Dockerfile also uses `COPY . .`; a local frontend `.env.*` or
   private-key file can therefore enter its builder and Vite build input.
4. The original PR #195 head `585cf145a514753242966d24e47496698c0ec671`
   and run `29283011617` proved the root marker only. That green run did not
   exercise the independent frontend context and is not final acceptance.

## Security invariant and preserved behavior

- Local `.env`, `.env.*`, private-key/certificate files, dependency folders,
  generated builds and local QA artifacts must not enter either Docker build
  context.
- The checked-in root `.env.example` remains available as the legitimate
  control, source files remain buildable and the final backend image contract
  remains unchanged.
- A Docker/context-policy-only change should run the Docker job without
  spending Backend and Frontend jobs; unknown paths still fail safe to the
  complete build.

## Implemented proof

- Root `.dockerignore` protects the backend context and preserves
  `.env.example`.
- `frontend/.dockerignore` independently protects the `./frontend` context.
- The backend Dockerfile validates after `COPY . .` that `.env.example` exists
  and no forbidden environment/private-key pattern reached `/app`.
- Required Docker CI creates one root environment marker, exact `.env` and
  `.env.local` frontend markers, one frontend private-key marker and one
  arbitrary file inside a frontend `secrets/` directory. It builds the final
  backend image once; the post-copy assertion proves root-context exclusion
  across the root and nested input classes.
- A second lightweight Alpine context-only build proves the frontend marker
  files are absent and `package.json` remains present. It does not repeat
  `npm ci` or the Vite build already owned by the Frontend job.
- Cleanup removes each exact marker path individually; there is no recursive or
  bulk deletion.

## Local verification

Passed:

```text
git diff --check
ruby -ryaml -e "YAML.load_file('.github/workflows/ci.yml')"
ruby -ryaml -e "d=YAML.load_file('.github/workflows/ci.yml'); s=d['jobs']['docker']['steps'].find{|x| x['name']=='Build backend image and verify Docker contexts'}; puts s['run']" | bash -n
```

An inline Bash case harness also passed six routing controls: docs-only,
root `.dockerignore`, frontend `.dockerignore`, frontend source, backend source
and unknown workflow paths.

The local machine has no `docker` command, so the realistic Docker-context
reproducer is deliberately not claimed locally. Exact-head GitHub `Docker
Build` is the acceptance gate. Because this PR also changes its workflow,
change routing, Backend Tests and Frontend Build must all report a successful
or intentional routed result on that same head before merge.
