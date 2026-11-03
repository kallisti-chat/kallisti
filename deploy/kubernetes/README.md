# Kubernetes template

`chart/` is a Helm chart for one public Kallisti installation on Kubernetes 1.26 or newer. It is not installable with its default values: there is no published application image, known storage capacity or backup destination in this repository. Do not apply the test fixture in `chart/ci/values.yaml`: it uses invalid image addresses, a nonexistent Secret and a fake backup acknowledgment.

## Cluster requirements

- A LoadBalancer implementation that supports TCP 80/443 and TCP plus UDP 64738 on the **same Service and external IP**. Verify this with the chosen provider before installing. Point one public DNS name at that IP; both web and native Mumble clients use it. Public ports 80 and 443 must reach Caddy for ACME issuance and renewal; native clients use 64738 directly. No ingress controller, host networking, privileged container or host path is required.
- A default StorageClass, or a specific class in `storage.className`, with two ReadWriteOnce claims on the same node: app data (SQLite, uploads, local backups and fallback Mumble identity) and Caddy's ACME account and certificates. The deployment has exactly one replica and a Recreate update strategy to avoid two SQLite writers. The storage driver must honor pod `fsGroup: 1000` or provision both volumes writable by UID/GID 1000. Test attach/detach and a restore before relying on the deployment.
- Both images: the published `ghcr.io/kallisti-chat/kallisti` and `ghcr.io/kallisti-chat/kallisti-cert-sync` (amd64 and arm64, a tag per release, for example `:0.10.1`), or your own builds of the root `Dockerfile` and `deploy/docker/Dockerfile.cert-sync`. Set `images.app` and `images.certSync` to immutable references: a version, preferably a digest (not `latest` or `edge`). Caddy is pinned to `caddy:2.11.3-alpine` by default. For a private registry, reference existing namespace-local image-pull Secrets as shown below; the chart does not create them.
- An existing namespace-local Secret named by `superuserSecretName`, with key `password`. Create it through your normal secret manager, not a plaintext file committed to Git. The password is still exposed to authorized cluster administrators and to the app process as an environment variable.
- An **off-cluster** backup of the app claim (or a consistent database snapshot plus uploads and identity files), Caddy state, and the Secret/configuration, with a tested restore. Kallisti's own daily SQLite snapshots are on the app claim and do not protect against storage loss. Set `backup.confirmed=true` only after this exists. Size both claims and each container's CPU/memory requests and limits for measured load; the chart will refuse missing values.

All three containers run as UID 1000 with read-only root filesystems. Caddy alone adds `NET_BIND_SERVICE` to bind 80/443; the copier reads Caddy's private store from a read-only mount and writes the app claim. The app never mounts the Caddy claim. Caddy sets security headers, compresses traffic, limits uploads to 11 MB and keeps access logs off because file URLs contain bearer keys. The copier validates the name, remaining validity and matching key every minute, then switches both files together. Kallisti uses its saved self-signed certificate until Caddy issues a valid one.

## Prepare and check

Create a private values file outside this repository based on `chart/values.yaml` and fill every required field. The chart does not install a backup controller, registry or load balancer. Confirm the DNS name, image digests, storage class and actual capacity, backup destination and measured CPU/memory needs before setting `backup.confirmed=true`.

For private images, add only references to existing registry Secrets in the **same namespace as the release's Pods**:

```yaml
imagePullSecrets:
  - name: regcred
```

The usual type is `kubernetes.io/dockerconfigjson`; manage the Secret content separately through your secret manager (for example SOPS or External Secrets). A Secret in another namespace cannot be used here. The chart does not create or own `regcred`, and uninstalling the chart does not remove it. These references authenticate kubelet image pulls for all three containers, not the app's `superuserSecretName`; they do not require enabling Pod API token automount. You can list multiple Secrets. Leave the list empty for public images or when pull authentication is supplied outside this chart.

```sh
helm lint deploy/kubernetes/chart -f /secure/path/kallisti-values.yaml --strict
helm template kallisti deploy/kubernetes/chart -n <namespace> -f /secure/path/kallisti-values.yaml
# Only after reviewing the rendered manifests and meeting the requirements:
helm upgrade --install kallisti deploy/kubernetes/chart -n <namespace> --create-namespace -f /secure/path/kallisti-values.yaml
```

## Optional separate uploads PVC

By default, uploads and media previews live at `/data/uploads` on the app PVC. To store them separately, uncomment the uploads PVC example at the end of [`templates/storage.yaml`](chart/templates/storage.yaml) and both uploads blocks (the app container's `volumeMounts` and the Pod's `volumes`) in [`templates/deployment.yaml`](chart/templates/deployment.yaml). Choose the uploads capacity and, optionally, its own `storageClassName` in the example. The app PVC still holds SQLite, local database backups and Mumble certificates; Caddy's storage is unchanged. No application setting changes are needed.

The uploads PVC must be mountable on the same node as the other claims and writable by UID/GID 1000, using a storage driver that honors `fsGroup` or pre-provisioned ownership. Run `helm lint` and review `helm template` output after editing. These are local chart customizations; preserve them when updating the chart.

For an existing installation, back up the data, stop Kallisti (scale the Deployment to zero), and copy the **contents** of the app PVC's `uploads` directory into the root of the new uploads PVC before starting with the new mount. Use your cluster's volume migration procedure and preserve ownership. An empty uploads mount hides the original files; it does not migrate them. If reverting to shared storage later, stop Kallisti and copy any new uploads back first. Back up and restore the separate uploads PVC alongside app and Caddy data: the built-in SQLite backups do not include uploaded files.

Helm uninstallation retains the app and Caddy PVCs, plus the optional uploads PVC, to avoid accidental data loss. It does not roll back database migrations or remove stored data. Read [the deployment guide](../../docs/deploy/kubernetes.md) for checks, updates and recovery. No cluster changes were made when writing this template.
