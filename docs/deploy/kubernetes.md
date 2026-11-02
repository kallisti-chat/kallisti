# Kubernetes deployment

The [Helm chart and prerequisites](../../deploy/kubernetes/README.md) describe a **reusable template**, not a live deployment. No cluster, image registry, storage class, size, expected user count or off-cluster backup destination was supplied. The chart therefore refuses to render with default values. Do not use its test-only values file for a real release.

## How it runs

A single Recreate Deployment contains Kallisti, Caddy and a certificate copier. One LoadBalancer Service must expose TCP 80/443 and TCP+UDP 64738 on the same external address. Set one public DNS name to that address. Caddy obtains HTTPS, serves HTTP and WebSocket connections to Kallisti on localhost, and stores certificates on its own persistent volume. The copier reads that volume and writes validated key pairs to the app volume; it does not run as root. Kallisti starts with a persistent self-signed Mumble certificate until the public certificate is available. Kallisti's `/data` claim holds SQLite, uploads, local backups and identity. Its HTTP readiness and liveness checks do not prove public ACME issuance or native Mumble reachability: verify both from outside the cluster.

The app and copier run with no capabilities. Caddy receives only `NET_BIND_SERVICE` to bind 80/443. Each has a read-only root filesystem. The Pod's `fsGroup` and UID are 1000. Confirm your storage driver applies writable group ownership to both claims before installing. The Service preserves client IP with `externalTrafficPolicy: Local`; the load balancer must route only to the ready node. No horizontal scaling is supported: a second independent app replica would race on SQLite and duplicate Mumble listeners.

## Release checks

Uploads can optionally use a separate PVC mounted at `/data/uploads`; see the [commented chart examples and migration steps](../../deploy/kubernetes/README.md#optional-separate-uploads-pvc). With this customization, check permissions and backups for all three claims, and verify uploads on the uploads claim instead of the app claim. The default storage layout remains unchanged.

After configuring real images, capacities, resources, the app's Secret, any namespace-local image-pull Secrets for private registries, and a tested off-cluster backup, run `helm lint` and `helm template` as shown in the chart README. Check your provider supports mixed-protocol LoadBalancer Services on one IP, and confirm DNS resolves to the allocated IP. Once an operator explicitly installs the chart, check:

```sh
kubectl -n <namespace> get deployment,service,pvc,pods
kubectl -n <namespace> rollout status deployment/kallisti
kubectl -n <namespace> logs deployment/kallisti -c cert-sync --tail=100
kubectl -n <namespace> logs deployment/kallisti -c caddy --tail=100
```

Also open `https://<your-domain>` in a browser, verify a trusted certificate and WebSocket chat, and connect a native Mumble client on TCP/UDP 64738. Confirm database backups and uploads are present in the app claim, and verify the off-cluster copy and restore procedure. Keep the secret outside Git; no plaintext Secret resource is in the chart. `helm.sh/resource-policy: keep` protects both PVCs on uninstall, but check your storage provider's reclaim policy too.

## Updates and rollback

Publish both immutable images, inspect the rendered chart diff, take and export a pre-update database backup, then upgrade. The app makes one-way database migrations at start. A Helm rollback only changes Kubernetes objects: an older binary may fail against the migrated database. To return to an older version, stop the Deployment, restore the database snapshot from before the upgrade (including SQLite WAL/SHM cleanup), then install the matching older images. Restore uploads and Mumble identity from the same recovery point if required. Caddy's claim holds its ACME account and certificates; retain it or expect reissuance. Do not delete or reformat PVCs as part of an ordinary rollback. See [Backups](../backups.md) for the database restore contract.
