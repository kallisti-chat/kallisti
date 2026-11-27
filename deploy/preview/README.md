# Kallisti pull-request previews

This separate Go module polls `kallisti-chat/kallisti` and maintains three
short-lived previews at `https://pr-N.kallisti.fans`. It is an infrastructure
controller, with a separate release and trust boundary from the application.
Keeping it here makes the tested image handoff and bootstrap schema reviewable
together. It does not use the Helm release chart, render PR-supplied YAML, run PR
scripts in the cluster, or deploy itself.

The code and local tests are ready for review. A separate fork pilot is used to
qualify hosted workflows and live infrastructure before upstream activation.
The upstream issue's working-address-in-15-minutes criterion is **not yet
demonstrated**; full CI, runner queues and image builds can exceed it.

## Admission and lifecycle

* Same-repository PRs targeting `main`, including drafts, are automatic. Fork
  publication requires `/preview <full current SHA>` from `cortexobscura` or
  `lambadalambda`. The command must be the entire unedited comment, apart from
  surrounding whitespace. A bare `/preview` is rejected with instructions to
  include the SHA; comment timestamps do not reliably establish the approved head.
  Build execution follows GitHub's normal pull-request approval policy, while
  publication and deployment require this separate explicit approval.
* The controller revalidates the original approval comment and durably binds its
  ID to PR+SHA. Deleted or edited approvals cannot start or promote a candidate.
* The original `test.yml`, `voice.yml` and `helm.yml` workflows must have latest
  successful `pull_request` runs for that head. Every reported commit check must
  be completed with success or skipped (for conditional jobs such as controller
  image publication on a PR), and combined commit statuses must be successful or
  absent. The required workflow runs must succeed; missing, neutral, cancelled
  and pending results fail closed.
  Concrete workflow IDs obtained from the known paths protect against a check
  with an attacker-chosen matching name. Newly added failing checks also block.
* Original PR workflows normally test GitHub's merge revision. `preview-build.yml`
  separately tests the **exact head SHA**: web type/tests, Go vet/race tests,
  desktop tests, chart checks and Chrome/Firefox/Safari voice tests. Only then
  does it build that head. Existing CI is left unchanged, so this deliberately
  duplicates work to establish explicit revision provenance.
* Automatic previews queue when all three slots are full. A manual approval
  evicts the oldest automatic preview, or the oldest approved one when all are
  approved. Evicted and expired admissions are suppressed until a new head or
  approval, preventing the same candidates from repeatedly evicting each other.
* Slots expire 72 hours after admission, not after each poll or commit. Closure
  or merge removes the route first, then the slice and revision Deployments.
  Foreground garbage collection and Pod disappearance must finish before reuse.
  API failures retry; state is not discarded on a failed cleanup.
* New commits keep the same URL. A single candidate rollout runs globally.
  Each slot can briefly contain its working revision plus one candidate, with
  separate `emptyDir` data. Failed builds, CI or ten-minute readiness timeouts
  leave the working revision selected. Failed candidates require a new head or
  approval to retry. A successful readiness check switches the endpoint, then
  deletes the old revision. An application failure **after** promotion does not
  automatically restore the old database; there is no post-promotion rollback.
* One owned PR comment reports status, URL and serving SHA. It is discovered by
  a stable marker and configured bot login after restart, then updated in place.
  No passwords appear in comments or state. Pending previews also have a status
  comment. The controller reloads durable state before each reconciliation.

## Image trust boundary

`.github/workflows/preview-build.yml` runs only on `pull_request`. It tests and
builds the exact PR head on fresh GitHub-hosted runners, with read-only permissions,
credentialless checkout, no registry login and no deployment credentials. This
keeps candidate execution in the PR context rather than the default branch's
privileged workflow context. Candidate code is untrusted, including approved PRs.

The build uploads a Docker archive. The separate trusted `preview.yml` runs from
`main` on `workflow_run` completion or an authorized `/preview <SHA>` comment. Its
resolver re-fetches GitHub run, workflow, PR and artifact data, checks the current
head and source run attempt, and enforces fork approval. Ordinary issue comments
do not start the resolver. It never checks out or executes candidate code.

A fresh publisher imports the validated artifact with `skopeo copy`, without
running its entrypoint or Dockerfile. Only publication has `packages: write`.
It computes the pushed manifest digest and writes `preview-metadata-ATTEMPT`.
Metadata binds the trusted publisher run/attempt to the source build workflow,
run/attempt and artifact ID, repository, PR/head, fixed image repository, digest
and approval. Images currently target `linux/amd64`.

The controller independently verifies both publisher and source-build provenance.
The publisher must use the configured workflow ID/path and trusted main history;
the source build must use `preview-build.yml`, a successful `pull_request` run,
current head and correct attempt/artifact. Legacy `pull_request_target` publication
is rejected. Metadata ZIPs are bounded, contain exactly one `metadata.json`, and
are never extracted or executed. GitHub API/Actions artifact authorization is the
provenance authority, not an independent signature or attestation verifier.
Protect `main`, workflow changes, publisher credentials and registry access.

The controller scans at most 1,000 successful preview runs from the preceding
96 hours; metadata retention is four days. Missing evidence fails closed. Registry
images need an operator-owned retention policy; this controller deletes cluster
runtime data, not registry packages or GitHub artifacts. A high-volume installation
should add an indexed handoff before increasing these scan bounds.

## Private test accounts

`server/cmd/preview-init` is built from trusted `main` into the controller image.
Flux pins that image by digest in `preview-system/preview-admission`, key
`bootstrapImage`. Admission independently requires that exact init image.

The init container reads Secret `preview-accounts`, keys `lambda` and `cortex`,
from files at `/accounts`, creates two ordinary password accounts and certificates,
closes registration (including guests), then exits before the application starts.
Passwords must be 16–256 characters; a single file-ending newline is removed.
No administrator password or role is assigned. The application mounts only `/data`,
not the Secret, API credentials or a service-account token. It necessarily can read
its own database and password hashes: use disposable **preview-only passwords**,
never production or personal credentials. An approved malicious app can also
observe passwords entered into its own login form.

A new revision/Pod starts with empty ephemeral data. Container restarts within the
same Pod keep its data; the init completion marker makes init retries safe. The
trusted bootstrap schema must be compatible with the candidate server. Older PRs
with incompatible schemas should fail readiness and keep the working preview.
The current checkout's seeded backend startup, both logins, and registration/guest
rejection are covered by the local smoke test. Arbitrary historical PR versions
are not promised compatible.

## Kubernetes contract

Flux owns namespaces, ServiceAccounts, RBAC, quotas, admission, NetworkPolicies,
Gateway, wildcard TLS, controller Deployment, bootstrap pin and credentials. The
controller owns the following runtime objects only:

| Namespace | Resource and names | API permissions |
| --- | --- | --- |
| `preview-system` | ConfigMap `preview-state` | get/create/update |
| `preview-system` | ConfigMap `preview-admission` | get only |
| `preview-system` | Lease `preview-controller` | get/create/update |
| `preview-slot-1`, `-2`, `-3` | Deployment `preview-PR-SHA12` | get/list/create/delete |
| Each slot | Pods; ReplicaSets | Pods get/list; ReplicaSets get |
| `preview-edge` | Service `preview-slot-N` | get/create; kept across admissions |
| `preview-edge` | EndpointSlice `preview-slot-N` | get/create/update/delete |
| `preview-edge` | HTTPRoute `preview-pr-N` | get/create/update/delete |

There is no Secret API access, namespace mutation, Pod deletion or native Mumble
port exposure. All runtime objects carry
`app.kubernetes.io/managed-by=kallisti-preview-controller`; slot objects carry
`preview.kallisti.fans/slot` as `1`, `2` or `3`. Pods use `preview-workload`, with
`automountServiceAccountToken: false`, UID/GID 1000, RuntimeDefault seccomp,
read-only root filesystems, no extra capabilities and no privilege escalation.

Each app/init container requests CPU `100m`, memory `128Mi`, ephemeral storage
`1Gi`; limits are `500m`, `256Mi`, `2Gi`. Init and app run sequentially. `/data` is
a disk-backed `emptyDir` limited to `1Gi`; `TMPDIR=/data/tmp` supports temporary
files without another mount. Media processing shares this small budget. Live
memory, upload, ffmpeg and disk-pressure behavior still require qualification.

The selectorless edge Service exposes named port `http`, TCP80 -> TCP8080. The
controller obtains a ready nonterminating IPv4 Pod **in the selected slot**, checks
its labels, image and tokenless SA, then follows Pod -> ReplicaSet -> Deployment
controller references with UID comparisons. It derives the EndpointSlice address
and targetRef itself, constrained by `POD_CIDRS`. No IP comes from GitHub metadata.
Pod replacement refreshes endpoints; loss of readiness clears them. The HTTPRoute
attaches to `preview-edge/public`, section `https`, using exactly
`pr-N.kallisti.fans`. Traefik sees only the edge namespace, not backend Secrets.

The generated Deployment, derived Pod, Service, EndpointSlice and HTTPRoute passed
server-side dry-run admission against the companion k0slab envelope in a disposable
Kubernetes 1.36.1 cluster. This proves that API contract, PSA and LimitRange match;
it does not prove live CNI/Traefik routing or application behavior on Kali.

## Operator configuration and credentials still needed

Build the trusted image with `docker build -f deploy/preview/Dockerfile .` from the
repository root. `preview-controller.yml` tests and publishes it only from upstream
`main`; pin its resulting digest in Flux and restart the controller after changing
the bootstrap pin. Install all preview workflows on protected `main` before activation.

The controller uses in-cluster authentication and Lease leadership. Its Deployment
must use SA `preview-controller` in `preview-system`. Because that SA defaults to
no token automount, explicitly project a Kubernetes API token and `kube-root-ca.crt`
at `/var/run/secrets/kubernetes.io/serviceaccount/{token,ca.crt}`. Do not give that
projection to preview workloads. Run one replica initially; the Lease permits
standby replicas. The 60-second lease has a 40-second renewal deadline and all
Kubernetes requests have a 15-second timeout. Requests use the leadership context.

Required environment:

| Variable | Meaning |
| --- | --- |
| `GITHUB_WORKFLOW_ID` | Numeric ID of installed upstream `preview.yml` |
| `GITHUB_BOT_LOGIN` | Exact API login used to own the single PR comment |
| `GITHUB_TOKEN_FILE` | Mounted file containing a repository-scoped API token |
| `POD_CIDRS` | Comma-separated allowed IPv4 Pod CIDRs, e.g. `10.248.0.0/16` |

The API token needs metadata/contents, Actions, checks/statuses and pull requests
read access, plus issue comments write access on **only** `kallisti-chat/kallisti`.
It does not need workflow dispatch, registry write, administration or cluster
permissions. A separately rotated GitHub App installation token is suitable; this
controller rereads the token file each poll and does not mint installation tokens.
Provide the disposable account Secret separately in each slot. GHCR images must be
publicly pullable, or the static envelope must separately configure pull credentials;
this implementation does not fetch or distribute registry secrets. Existing wildcard
DNS/TLS, Gateway/CNI policy and any tailnet ACL remain operator responsibilities.

No secrets were obtained by this change. These are deployment prerequisites, not
instructions to grant broad credentials or activate the controller now.

## Verification

From this directory:

```
make check                    # gofmt, go vet, Go race tests
make workflow-check           # Python 3 + PyYAML 6.0.3 + Node.js
make smoke                    # builds current backend/init into a temporary directory
make build
```

`golangci-lint run ./...` and actionlint 1.7.12 pass for the new code/workflows.
The unit/fake-API tests cover admission/eviction/expiry, retrying deletion, failed
and stale candidates, approval replay, artifact run/attempt/repository mismatch,
additional failed checks, comment recovery, Pod ownership and replacement. Workflow
tests execute the actual resolver JavaScript with delayed/same-second comment,
explicit SHA, edited approval, attacker and draft/fork cases. No hosted Actions run
or actual registry publish/import has been tested by these local checks.

To export actual Kubernetes fixtures for the companion admission test:

```
PREVIEW_FIXTURE_DIR=/tmp/kallisti-preview-fixtures go test ./internal/controller -run TestDeploymentIsolationAndRenderFixtures -count=1
```

Remaining live acceptance: GitHub run/ref and artifact handoff, registry visibility,
real hostname/TLS and Traefik selectorless EndpointSlice support, two-browser text
and uploads, two-browser voice over WebSocket fallback, time-to-preview, candidate
OOM/readiness failure, Pod recreation, close/merge cleanup, controller failover,
network denial to private/cluster/tailnet addresses and outbound public internet.
Screen sharing is **not** covered by the existing voice tests and needs a separate
qualification session. Do not mark issue #33 complete from local tests alone.

GitHub or API outages delay reconciliation and TTL cleanup: the current loop
requires a successful GitHub snapshot before advancing lifecycle state.
