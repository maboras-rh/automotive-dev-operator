# CRC cluster with Automotive Dev Operator, Jumpstarter, and a QEMU exporter

Bring up one OpenShift Local (CRC) cluster and run all of the following on it:

- Automotive Dev Operator (existing `hack/crc` scripts)
- Jumpstarter controller, router, and login route
- One privileged QEMU exporter pod (virtual target)
- A single `caib image build-dev --flash` against that exporter

This document is a runbook. It does not change operator or Jumpstarter source, and it does not commit anything.

Do **not** run `jmp admin create cluster`. That command creates a separate kind or minikube cluster. Jumpstarter must be installed onto the CRC cluster that is already in `KUBECONFIG`.

## What already exists

| Piece | Where | What it does |
|-------|--------|----------------|
| CRC + operator | `hack/crc/` | `01-prep-host.sh`, `04-expose-default-registry.sh`, `02-deploy-operator.sh`, `03-crc-operator-sanity.sh` |
| Jumpstarter operator | `jumpstarter` repo, release `v0.9.0` | Install from the published `operator-installer.yaml`. Do not build the operator from the local checkout for this bring-up. |
| QEMU exporter | Not in the CRC scripts | Manual privileged pod, same shape as the `autodev2` runbook, with CRC-sized guest memory |

## Layout

```
laptop
  crc oc-env / jmp / caib
        |
        v
CRC VM (OpenShift)
  automotive-dev-operator-system   operator, Build API, Tekton
  openshift-image-registry         push target for built images
  cert-manager                     TLS for Jumpstarter
  jumpstarter-operator-system      Jumpstarter operator
  jumpstarter-lab                  controller, router, QEMU exporter pod
```

## 0. Decisions locked for this plan

| Topic | Choice | Why |
|-------|--------|-----|
| Cluster | Existing CRC flow in `hack/crc` | One cluster, already tested for the operator |
| Jumpstarter install | Release `v0.9.0` operator installer + `Jumpstarter` CR | Supported OpenShift path. `jmp admin create cluster` is the wrong tool |
| `jmp` CLI | `release-0.9`, not `main` | The CLI must match the controller. A `0.10` dev CLI against a `0.9` controller is an unnecessary skew |
| Auth | Internal Jumpstarter tokens (`jmp admin create client --save`) | CRC has no Red Hat SSO. External OIDC is out of scope |
| QEMU | One privileged Pod, image `quay.io/rh-automotive-lab/jumpstarter-qemu:latest` | Same exporter model already proven on `autodev2` |
| Guest size | 2 vCPU, 2G RAM | CRC's default 12 GiB VM cannot host an 8G QEMU guest plus OpenShift |
| CAIB arch | `arm64` on Apple Silicon CRC, `amd64` on x86_64 CRC | Match the CRC node architecture |
| Code changes | None | Gaps are covered by the shell script in the appendix, which only applies cluster manifests |

### Known constraint: internal tokens and `jmp login`

Controller-issued tokens use issuer `https://localhost:8085`. That address exists only inside the controller pod.

The flash task runs `jmp login --nointeractive` and, in current `flash_image.sh`, treats a login failure as a warning and continues with the token already in the client file. That is the CRC path:

- Create the client with `jmp admin create client --save` immediately before the flash.
- Expect a warning about `localhost:8085` in the flash pod log.
- Do not switch this CRC lab to OIDC unless an identity provider is added later.

If lease creation prints `Token is expired` and the lease name is not a UUID, stop and recreate the client. Do not retry flash on top of a bad lease.

### Known constraint: KVM inside CRC

The QEMU pod is privileged so it can use `/dev/kvm` when the CRC node exposes it.

- Linux CRC on KVM: nested virt sometimes works. Check inside the exporter pod.
- macOS CRC (vfkit): `/dev/kvm` is often absent. QEMU then uses TCG emulation. Boot and flash still prove the wiring, and they are slow.

```bash
oc -n jumpstarter-lab exec manual-qemu-aarch64 -- ls -l /dev/kvm || true
```

## 1. Host prerequisites

CRC's own minimum (4 CPU, 12 GiB, 50 GiB disk) is too small once Jumpstarter, cert-manager, a privileged QEMU pod, and an image build are added.

| Host | Give CRC |
|------|----------|
| 16 cores / 32 GiB | `--cpus 6 --memory 20480 --disk-size 90` |
| 32 cores / 64 GiB | `--cpus 8 --memory 24576 --disk-size 100` |

Also required:

- Pull secret from <https://console.redhat.com/openshift/create/local>
- `sudo` on Linux (the CRC scripts require it)
- `podman` (installed by `01-prep-host.sh` on Linux; `podman machine start` on macOS before registry expose and image build)
- Outbound access to `quay.io`, `github.com`, and `mirror.openshift.com`
- About 90 GiB free on the host disk that holds the CRC VM

Confirm the architecture. The QEMU firmware block in this plan is AAVMF (aarch64).

```bash
uname -sm
```

| Host | CRC node | `caib --arch` | QEMU firmware in this plan |
|------|----------|---------------|----------------------------|
| macOS arm64 | aarch64 | `arm64` | AAVMF (this document) |
| Linux x86_64 | x86_64 | `amd64` | Do not use the AAVMF block below. Use an x86 OVMF exporter config, or expect very slow aarch64 emulation |

## 2. Install `jmp` and `caib` on the laptop

`oc` comes from CRC (`eval "$(crc oc-env)"`). Install the other two before the cluster steps so version skew is fixed up front.

### `jmp` from the 0.9 release line

```bash
curl -fsSL https://raw.githubusercontent.com/jumpstarter-dev/jumpstarter/main/python/install.sh | bash -s - -s release-0.9
source ~/.local/jumpstarter/set
jmp version
```

`jmp version` should report a `0.9.x` build, not `0.10.0.dev`.

If a `main` install is already active, remove it first so the shell does not keep the dev CLI:

```bash
rm -rf ~/.local/jumpstarter
```

Then rerun the installer with `-s release-0.9`.

Optional persistence:

```bash
echo 'source ~/.local/jumpstarter/set' >> ~/.zshrc
```

### `caib`

From a released binary:

```bash
curl -fsSL https://raw.githubusercontent.com/centos-automotive-suite/automotive-dev-operator/main/hack/install-caib.sh | bash
caib version
```

Or from the local tree, without installing over a release binary:

```bash
cd /Users/maboras/rh_workspace/aib/automotive-dev-operator_main
make build-caib
export PATH="$PWD/bin:$PATH"
caib version
```

## 3. Start CRC and deploy the Automotive Dev Operator

Work from the operator repo. Do not point these scripts at the Jumpstarter repo.

```bash
cd /Users/maboras/rh_workspace/aib/automotive-dev-operator_main
```

### Linux

```bash
sudo CRC_CUSTOM_ARGS="--cpus 6 --memory 20480 --disk-size 90" \
  bash hack/crc/setup-crc.sh /path/to/pull-secret.txt
```

### macOS

```bash
CRC_CUSTOM_ARGS="--cpus 6 --memory 20480 --disk-size 90" \
  bash hack/crc/setup-crc.sh /path/to/pull-secret.txt
```

`setup-crc.sh` runs, in order:

1. `01-prep-host.sh` — install CRC, `crc setup`, `crc start`
2. `04-expose-default-registry.sh` — external route for the internal registry
3. `02-deploy-operator.sh` — OpenShift Pipelines, OpenShift Builds, operator catalog deploy, `aib=true` node label, OperatorConfig registry patch
4. `03-crc-operator-sanity.sh` — deployment checks

If CRC is already running and only the operator is missing, skip phase 1 and run `04` then `02`.

Changing CPU, memory, or disk after the VM exists requires `crc stop && crc delete -f` and a new `crc start`. `crc start` does not resize an existing VM.

### Point the shell at CRC

```bash
eval "$(crc oc-env)"
export KUBECONFIG="${KUBECONFIG:-$HOME/.crc/machines/crc/kubeconfig}"
oc whoami --show-server
```

Expected API: `https://api.crc.testing:6443`.

```bash
oc get pods -n automotive-dev-operator-system
oc get operatorconfig config -n automotive-dev-operator-system
oc get route ado-build-api -n automotive-dev-operator-system
```

OperatorConfig phase must be `Ready` before the flash steps.

Record the ingress domain. On CRC this is `apps-crc.testing`:

```bash
oc get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}{"\n"}'
```

Every Jumpstarter route hostname must end with that domain. A wrong domain is the same failure mode as the missing `rosa.` label on `autodev2`: routes exist, and nothing can resolve them.

## 4. Install cert-manager on the CRC cluster

The Jumpstarter CR in this plan sets `spec.certManager.enabled: true`. CRC does not ship cert-manager.

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml
kubectl wait --namespace cert-manager \
  --for=condition=available deployment/cert-manager-webhook \
  --timeout=180s
```

`kubectl` here is the same cluster as `oc`. If `kubectl` is not installed, use `oc apply` and `oc wait` with the same arguments.

## 5. Install the Jumpstarter operator on CRC

```bash
oc apply -f https://github.com/jumpstarter-dev/jumpstarter/releases/download/v0.9.0/operator-installer.yaml
oc wait --namespace jumpstarter-operator-system \
  --for=condition=available deployment/jumpstarter-operator-controller-manager \
  --timeout=180s
oc get crd jumpstarters.operator.jumpstarter.dev exporters.jumpstarter.dev
```

## 6. Create the Jumpstarter service in `jumpstarter-lab`

Set `baseDomain` explicitly. If it is left empty, the operator fills `jumpstarter.<namespace>.<ingress-domain>`, which is also valid, but an explicit value makes the route hosts obvious and avoids repeating the `autodev2` DNS mistake.

`apps-crc.testing` is resolved by CRC's DNS on the laptop (`address=/apps-crc.testing/<vm-ip>`), including nested names.

```bash
export JUMPSTARTER_NAMESPACE=jumpstarter-lab
export JS_BASE_DOMAIN="${JUMPSTARTER_NAMESPACE}.$(oc get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}')"
echo "$JS_BASE_DOMAIN"
# expected: jumpstarter-lab.apps-crc.testing

oc create namespace "$JUMPSTARTER_NAMESPACE" --dry-run=client -o yaml | oc apply -f -

cat <<EOF | oc apply -f -
apiVersion: operator.jumpstarter.dev/v1alpha1
kind: Jumpstarter
metadata:
  name: jumpstarter
  namespace: ${JUMPSTARTER_NAMESPACE}
spec:
  baseDomain: ${JS_BASE_DOMAIN}
  certManager:
    enabled: true
    server:
      selfSigned:
        enabled: true
  authentication:
    internal:
      prefix: "internal:"
      enabled: true
  controller:
    image: quay.io/jumpstarter-dev/jumpstarter-controller:0.9.0
    imagePullPolicy: IfNotPresent
    replicas: 1
    grpc:
      endpoints:
        - address: grpc.${JS_BASE_DOMAIN}:443
          route:
            enabled: true
    login:
      endpoints:
        - address: login.${JS_BASE_DOMAIN}:443
          route:
            enabled: true
  routers:
    image: quay.io/jumpstarter-dev/jumpstarter-controller:0.9.0
    imagePullPolicy: IfNotPresent
    replicas: 1
    grpc:
      endpoints:
        - address: router-0.${JS_BASE_DOMAIN}:443
          route:
            enabled: true
EOF
```

Wait until the controller and router pods are Ready:

```bash
oc -n "$JUMPSTARTER_NAMESPACE" get pods,route,certificate
oc -n "$JUMPSTARTER_NAMESPACE" wait --for=condition=Ready pod -l app=jumpstarter-controller --timeout=300s
```

Expected routes (names can vary; hosts must match):

| Role | Host |
|------|------|
| Controller gRPC | `grpc.jumpstarter-lab.apps-crc.testing` |
| Router gRPC | `router-0.jumpstarter-lab.apps-crc.testing` |
| Login | `login.jumpstarter-lab.apps-crc.testing` |

gRPC routes use TLS passthrough. The login route uses edge TLS. Both are created by the Jumpstarter operator. Do not front them with a second Ingress.

### Confirm DNS from the laptop and from a pod

```bash
getent hosts "grpc.${JS_BASE_DOMAIN}" || dscacheutil -q host -a name "grpc.${JS_BASE_DOMAIN}"

oc -n "$JUMPSTARTER_NAMESPACE" run dnscheck --rm -it --restart=Never \
  --image=registry.access.redhat.com/ubi9/ubi-minimal:latest \
  -- getent hosts "grpc.${JS_BASE_DOMAIN}"
```

The laptop lookup must return the CRC VM address. The pod lookup must return an address too. Flash pods dial this hostname. If the pod lookup fails, stop and apply the DNS patch in section 7 before creating the exporter.

## 7. In-cluster DNS patch (only if section 6 failed inside the pod)

CRC teaches the laptop resolver about `apps-crc.testing`. Pods use CoreDNS, which sometimes does not.

This patch is cluster configuration, not a source change. It adds a CoreDNS rewrite only when the check above failed.

```bash
CRC_IP=$(crc ip)
oc -n openshift-dns get configmap dns-default -o yaml > /tmp/dns-default.yaml
```

If `grpc.${JS_BASE_DOMAIN}` does not resolve in a pod, add a hosts stanza to the CoreDNS Corefile in `openshift-dns` / `dns-default` that maps these names to the ingress router, then roll the DNS daemonset.

Prefer this check first, because many CRC builds already forward `apps-crc.testing` and the patch is unnecessary:

```bash
oc get svc -n openshift-ingress router-internal-default
```

If a patch is required, the smallest one is a `hosts` plugin entry in CoreDNS for the three Jumpstarter names, pointing at the router ClusterIP. Take a backup of the ConfigMap before editing it. Restart:

```bash
oc -n openshift-dns rollout restart daemonset/dns-default
```

Re-run the pod `getent` from section 6 before continuing.

## 8. Environment for the exporter and for CAIB

```bash
source ~/.local/jumpstarter/set
eval "$(crc oc-env)"

export JUMPSTARTER_NAMESPACE=jumpstarter-lab
export EXPORTER_NAME=manual-qemu-aarch64
export POD_NAME="$EXPORTER_NAME"
export CONFIG_SECRET_NAME="${EXPORTER_NAME}-config"
export QEMU_IMAGE=quay.io/rh-automotive-lab/jumpstarter-qemu:latest
export RUNBOOK_DIR="${HOME}/tmp/crc-qemu-exporter"
export CAIB_ENDPOINT="https://$(oc -n automotive-dev-operator-system get route ado-build-api -o jsonpath='{.spec.host}')"
export REGISTRY_IMAGE="image-registry.openshift-image-registry.svc:5000/automotive-dev-operator-system/qemu-images"

mkdir -p "$RUNBOOK_DIR"
echo "CAIB_ENDPOINT=$CAIB_ENDPOINT"
```

Push stays on the CRC internal registry, which the operator is already configured to use (`clusterRegistryRoute`). You do not need a Quay repo for this lab. `caib` still needs `--insecure` when it talks to the CRC registry route from the laptop. The flash task pulls the image from inside the cluster, so the image reference passed to flash must be the in-cluster registry host, not the external route host.

## 9. Create the Jumpstarter client (internal token)

```bash
jmp admin create client crc-caib \
  --namespace "$JUMPSTARTER_NAMESPACE" \
  --insecure-tls \
  --save \
  --nointeractive
```

Copy it for CAIB. Keep this file out of git.

```bash
cp ~/.config/jumpstarter/clients/crc-caib.yaml \
  "$RUNBOOK_DIR/crc-caib.client.yaml"
chmod 600 "$RUNBOOK_DIR/crc-caib.client.yaml"
```

Sanity check from the laptop:

```bash
jmp get exporters --client crc-caib --insecure-tls || true
```

An empty exporter list is fine at this point. A connection error means section 6 or 7 is not done.

`jmp login --nointeractive` against this client is expected to fail with `localhost:8085`. That does not block `jmp get` or lease creation while the token in the file is still valid. Do not replace this client with an OIDC login on CRC.

## 10. Create the exporter object

```bash
oc -n "$JUMPSTARTER_NAMESPACE" delete exporter "$EXPORTER_NAME" --ignore-not-found

jmp admin create exporter "$EXPORTER_NAME" \
  --namespace "$JUMPSTARTER_NAMESPACE" \
  --insecure-tls \
  --save \
  --nointeractive \
  --label runbook=manual-qemu \
  --label target=qemu \
  --label board-type=virtual \
  --label enabled=true
```

This writes a stub to `~/.config/jumpstarter/exporters/manual-qemu-aarch64.yaml`. The stub has `endpoint`, `tls`, and `token`. It does not start QEMU.

## 11. Render the full QEMU ExporterConfig

The script copies `endpoint`, `token`, and the CA from the stub, then adds the drivers the lease shell and the flash task call:

| Export | Lease command | Role |
|--------|----------------|------|
| `serial` → `qemu.console` | `j serial pipe` | Boot log |
| `storage` → `qemu.flasher` | `j storage flash oci://...` | Flash |
| `power` → `qemu.power` | `j qemu power on` / `off` | Start and stop the guest |
| `qemu` | `j qemu resize ...` | vCPU, RAM, firmware, SSH hostfwd |

Guest RAM is **2G** and vCPU count is **2**. Do not copy the `autodev2` `8G` / `smp: 4` block onto a 20 GiB CRC VM.

```bash
python3 - <<PY
import pathlib, re, sys, os

stub = pathlib.Path.home() / ".config/jumpstarter/exporters/manual-qemu-aarch64.yaml"
out = pathlib.Path(os.environ["RUNBOOK_DIR"]) / "config.yaml"

if not stub.exists():
    sys.exit(f"Stub not found: {stub}")

text = stub.read_text()

def must(pat, desc):
    m = re.search(pat, text, re.MULTILINE)
    if not m:
        sys.exit(f"Missing {desc} in {stub}")
    return m.group(1).strip()

endpoint = must(r'^endpoint:\s*(.+)$', 'endpoint')
token = must(r'^token:\s*(.+)$', 'token')

ca_match = re.search(r'^\s+ca:\s*(.+)$', text, re.MULTILINE)
tls_block = f"  ca: {ca_match.group(1).strip()}\n  insecure: true" if ca_match else "  insecure: true"

out.write_text(f"""apiVersion: jumpstarter.dev/v1alpha1
kind: ExporterConfig
metadata:
  namespace: jumpstarter-lab
  name: manual-qemu-aarch64
endpoint: {endpoint}
tls:
{tls_block}
token: {token}
export:
  serial:
    ref: qemu.console
  storage:
    ref: qemu.flasher
  power:
    ref: qemu.power
  qemu:
    type: jumpstarter_driver_qemu.driver.Qemu
    config:
      smp: 2
      mem: "2G"
      default_partitions:
        OVMF_CODE.fd: /usr/share/AAVMF/AAVMF_CODE.fd
        OVMF_VARS.fd: /usr/share/AAVMF/AAVMF_VARS.fd
      hostfwd:
        ssh:
          hostport: 9022
          guestport: 22
""")
print(f"Wrote {out}")
PY

chmod 600 "$RUNBOOK_DIR/config.yaml"
```

Check that `endpoint` is `grpc.jumpstarter-lab.apps-crc.testing:443` (or the host you set in section 6), not a stale hostname.

The file contains the exporter token. Do not commit it.

## 12. Privileged SCC, Secret, and exporter Pod

OpenShift refuses a privileged container unless the pod's service account is in the `privileged` SCC. This pod uses the `default` service account in `jumpstarter-lab`.

```bash
oc adm policy add-scc-to-user privileged \
  -z default \
  -n "$JUMPSTARTER_NAMESPACE"

oc -n "$JUMPSTARTER_NAMESPACE" delete pod "$POD_NAME" --ignore-not-found
oc -n "$JUMPSTARTER_NAMESPACE" delete secret "$CONFIG_SECRET_NAME" --ignore-not-found

oc -n "$JUMPSTARTER_NAMESPACE" create secret generic "$CONFIG_SECRET_NAME" \
  --from-file=config.yaml="$RUNBOOK_DIR/config.yaml"

cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: ${POD_NAME}
  namespace: ${JUMPSTARTER_NAMESPACE}
  labels:
    app: jumpstarter-qemu-exporter
    runbook: manual-qemu
spec:
  restartPolicy: Always
  containers:
    - name: exporter
      image: ${QEMU_IMAGE}
      command: ["/jumpstarter/bin/jmp", "run", "--exporter-config", "/etc/jumpstarter/exporters/config.yaml"]
      securityContext:
        privileged: true
      resources:
        requests:
          cpu: "1"
          memory: 2Gi
        limits:
          cpu: "2"
          memory: 4Gi
      volumeMounts:
        - name: exporter-config
          mountPath: /etc/jumpstarter/exporters
          readOnly: true
        - name: qemu-data
          mountPath: /var/lib/jumpstarter
  volumes:
    - name: exporter-config
      secret:
        secretName: ${CONFIG_SECRET_NAME}
    - name: qemu-data
      emptyDir:
        sizeLimit: 12Gi
EOF

oc -n "$JUMPSTARTER_NAMESPACE" wait --for=condition=Ready "pod/${POD_NAME}" --timeout=5m
oc -n "$JUMPSTARTER_NAMESPACE" get exporter "$EXPORTER_NAME" -w
```

Wait until status is `Available` (or `LeaseReady` when a lease is held). `Offline` with no pod, or `CrashLoopBackOff`, is not a client problem. Read the pod log:

```bash
oc -n "$JUMPSTARTER_NAMESPACE" describe pod "$POD_NAME"
oc -n "$JUMPSTARTER_NAMESPACE" logs "$POD_NAME" --tail=80
```

Image pull failures on `quay.io/rh-automotive-lab/jumpstarter-qemu` mean the cluster pull secret cannot read that repo. Confirm the image is public, or add a pull secret and set `spec.imagePullSecrets` on the pod. That is still a manifest change, not a source change.

## 13. Tell the operator how to flash `target=qemu`

`caib --target qemu` reads `OperatorConfig.spec.jumpstarter.targetMappings.qemu`. Without this patch, flash has no exporter selector.

```bash
oc patch operatorconfig config -n automotive-dev-operator-system --type=merge -p '
spec:
  jumpstarter:
    namespace: jumpstarter-lab
    image: quay.io/jumpstarter-dev/jumpstarter:0.9.0
    defaultLeaseDuration: "01:00:00"
    targetMappings:
      qemu:
        selector: "runbook=manual-qemu,target=qemu"
        flashCmd: "j storage flash oci://{image_uri}"
'
oc get operatorconfig config -n automotive-dev-operator-system \
  -o jsonpath='{.spec.jumpstarter.targetMappings.qemu}{"\n"}{.status.phase}{"\n"}'
```

`image` is the CLI image used by the flash TaskRun. Keep it on `0.9.0` so it matches the controller installed in section 6.

Wait until `status.phase` is `Ready` again after the patch.

## 14. Power smoke test

Release any leftover lease first. A held lease makes the later flash sit on `already leased`.

```bash
jmp get leases --client crc-caib --insecure-tls
# jmp delete leases --client crc-caib --insecure-tls <lease-id>

jmp shell --client crc-caib --insecure-tls \
  --exporter "$EXPORTER_NAME" \
  --duration 5m \
  -- sh -c 'j qemu power on && j qemu power off'
```

Then confirm the exporter is free:

```bash
jmp get leases --client crc-caib --insecure-tls
oc -n "$JUMPSTARTER_NAMESPACE" get exporter "$EXPORTER_NAME"
```

## 15. Build and flash with one `caib` command

Use a small manifest. `maintenance/smoke/images/simple.aib.yml` in lab-management is the one used on `autodev2`. If that tree is not checked out, any AIB manifest that builds a qcow2 is fine. Shrink `image.image_size` toward 4 GiB if the CRC disk is tight. The guest firmware and exporter disk volume must be able to hold the image.

Refresh nothing via OIDC. Recreate the client file if it is hours old, then copy it again to `$RUNBOOK_DIR/crc-caib.client.yaml`.

```bash
export CLIENT_CONFIG="$RUNBOOK_DIR/crc-caib.client.yaml"

caib image build-dev /path/to/simple.aib.yml \
  --server "$CAIB_ENDPOINT" \
  --arch arm64 \
  --target qemu \
  --mode package \
  --format qcow2 \
  --push "${REGISTRY_IMAGE}:qemu" \
  --insecure \
  --flash \
  --client "$CLIENT_CONFIG" \
  --lease-duration 01:00:00
```

On an x86_64 CRC node, change `--arch arm64` to `--arch amd64` only if the exporter config is an x86 QEMU, not the AAVMF config from section 11.

If the build fails with `registries.conf must be in v2 format but is in v1`, the AIB image in OperatorConfig is the broken `1.3.4` line. Pin a known-good image on the command and do not patch the operator:

```bash
  --aib-image quay.io/centos-sig-automotive/automotive-image-builder:1.3.2 \
```

The current API default in this repo is `quay.io/centos-sig-automotive/automotive-image-builder:1.3.5`. Try the cluster default first.

### Watch the flash task

```bash
oc -n automotive-dev-operator-system get taskrun
oc -n automotive-dev-operator-system logs -l tekton.dev/task=flash-image -f
```

Success is all of `build-image`, `push-disk-artifact`, and `flash-image` in `Succeeded`.

The flash log should show:

1. `WARNING` from `jmp login` about `localhost:8085`, then continuation
2. `Lease acquired: <uuid>` as a single UUID line
3. `j storage flash oci://image-registry.openshift-image-registry.svc:5000/...`

## 16. Open the flashed guest

```bash
jmp shell --client crc-caib --insecure-tls \
  --exporter "$EXPORTER_NAME" \
  --duration 30m
```

Inside the lease:

```bash
j qemu power on
j serial pipe
# wait for a login prompt, then Ctrl+C
j qemu power off
exit
```

Release the lease when finished so the next flash can bind:

```bash
jmp get leases --client crc-caib --insecure-tls
jmp delete leases --client crc-caib --insecure-tls <lease-id>
```

## 17. Tear down (optional)

```bash
jmp delete leases --client crc-caib --insecure-tls <lease-id>
oc -n jumpstarter-lab delete pod manual-qemu-aarch64 --ignore-not-found
oc -n jumpstarter-lab delete secret manual-qemu-aarch64-config --ignore-not-found
oc -n jumpstarter-lab delete exporter manual-qemu-aarch64 --ignore-not-found
```

Leave the Jumpstarter CR and the operator in place if you will flash again. To remove Jumpstarter:

```bash
oc -n jumpstarter-lab delete jumpstarter jumpstarter
oc delete -f https://github.com/jumpstarter-dev/jumpstarter/releases/download/v0.9.0/operator-installer.yaml
```

CRC itself:

```bash
# Linux
sudo bash hack/crc/crc-cleanup.sh
# macOS
bash hack/crc/crc-cleanup.sh
```

## Troubleshooting

| Symptom | What it means | What to do |
|---------|----------------|------------|
| `jmp admin create cluster` was used | A second cluster exists | Delete that kind/minikube cluster. Keep using `crc oc-env` |
| Exporter `Offline`, pod missing | Pod was deleted or never applied | Repeat section 12 |
| Pod `Forbidden` / SCC | `privileged` SCC not granted | Repeat the `add-scc-to-user` command in section 12 |
| Routes exist, laptop cannot resolve them | `baseDomain` is not under `apps-crc.testing` | Fix the CR `baseDomain`, delete the exporter, recreate sections 10–12 so the stub picks up the new endpoint |
| Flash pod cannot resolve `grpc....apps-crc.testing` | In-cluster DNS | Section 7 |
| Flash log: `localhost:8085` | Expected with internal tokens | Continue if the next line still creates a lease UUID |
| `leases.jumpstarter.dev "Token" not found` | Lease id captured the re-auth banner | Recreate the client, delete bad leases, flash again |
| Flash waits: `already leased` | Smoke-test shell still holds the exporter | `jmp delete leases` for the other lease |
| Build `registries.conf` v1 | Bad AIB image | `--aib-image ...:1.3.2` |
| QEMU extremely slow or `kvm` errors | No `/dev/kvm` in the CRC VM | Expected on macOS CRC. TCG still validates the pipeline |
| Node disk pressure during build | CRC disk too small | Recreate CRC with `--disk-size 90` or larger |
| Flash image pull 401 inside the exporter | Exporter pod cannot read the internal registry | `oc policy add-role-to-user system:image-puller system:serviceaccount:jumpstarter-lab:default -n automotive-dev-operator-system` |

## Order of operations (checklist)

1. Install `jmp` from `release-0.9` and `caib`.
2. Start CRC with at least 20 GiB RAM and 90 GiB disk (`hack/crc/setup-crc.sh`).
3. Confirm operator namespace Ready and `ado-build-api` route.
4. Install cert-manager `v1.21.2`.
5. Install Jumpstarter operator `v0.9.0` onto CRC.
6. Apply the `Jumpstarter` CR with `baseDomain` = `jumpstarter-lab.<ingress domain>`.
7. Resolve `grpc.<baseDomain>` on the laptop and from a pod.
8. Create internal client `crc-caib`.
9. Create exporter `manual-qemu-aarch64` and render `config.yaml` (2G guest).
10. Grant `privileged` SCC, create the Secret, apply the Pod, wait for `Available`.
11. Patch OperatorConfig `targetMappings.qemu`.
12. `j qemu power on && j qemu power off` with no leftover lease.
13. `caib image build-dev --flash`.
14. `j serial pipe` on a new lease, then delete the lease.

## Appendix: helper script

Save this as `hack/crc/05-jumpstarter-qemu.sh` only if you want one command for sections 4–13 after CRC and the Automotive Dev Operator are already up. It is not part of `setup-crc.sh`, and it does not modify Git. Review it before running. It refuses to run unless `oc whoami --show-server` contains `crc.testing`.

The script is intentionally not added to the repo by this plan. Copy it from this appendix when you want it.

```bash
#!/usr/bin/env bash
# 05-jumpstarter-qemu.sh
# Install Jumpstarter v0.9.0 on an existing CRC cluster and start one QEMU exporter.
# Prerequisite: hack/crc/02-deploy-operator.sh has already succeeded.
# Does not modify git checkouts.
set -euo pipefail

JS_VERSION=v0.9.0
CM_VERSION=v1.21.2
NS=jumpstarter-lab
EXPORTER_NAME=manual-qemu-aarch64
QEMU_IMAGE=quay.io/rh-automotive-lab/jumpstarter-qemu:latest
RUNBOOK_DIR="${RUNBOOK_DIR:-$HOME/tmp/crc-qemu-exporter}"

eval "$(crc oc-env)"
export KUBECONFIG="${KUBECONFIG:-$HOME/.crc/machines/crc/kubeconfig}"
source "${HOME}/.local/jumpstarter/set"

server=$(oc whoami --show-server)
case "$server" in
  *crc.testing*) ;;
  *) echo "Refusing to run: current server is $server" >&2; exit 1 ;;
esac

echo "Cluster: $server"
echo "User: $(oc whoami)"

oc apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CM_VERSION}/cert-manager.yaml"
oc wait --namespace cert-manager --for=condition=available deployment/cert-manager-webhook --timeout=180s

oc apply -f "https://github.com/jumpstarter-dev/jumpstarter/releases/download/${JS_VERSION}/operator-installer.yaml"
oc wait --namespace jumpstarter-operator-system \
  --for=condition=available deployment/jumpstarter-operator-controller-manager --timeout=180s

domain=$(oc get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}')
base="${NS}.${domain}"

oc create namespace "$NS" --dry-run=client -o yaml | oc apply -f -
cat <<EOF | oc apply -f -
apiVersion: operator.jumpstarter.dev/v1alpha1
kind: Jumpstarter
metadata:
  name: jumpstarter
  namespace: ${NS}
spec:
  baseDomain: ${base}
  certManager:
    enabled: true
    server:
      selfSigned:
        enabled: true
  authentication:
    internal:
      prefix: "internal:"
      enabled: true
  controller:
    image: quay.io/jumpstarter-dev/jumpstarter-controller:0.9.0
    imagePullPolicy: IfNotPresent
    replicas: 1
    grpc:
      endpoints:
        - address: grpc.${base}:443
          route:
            enabled: true
    login:
      endpoints:
        - address: login.${base}:443
          route:
            enabled: true
  routers:
    image: quay.io/jumpstarter-dev/jumpstarter-controller:0.9.0
    imagePullPolicy: IfNotPresent
    replicas: 1
    grpc:
      endpoints:
        - address: router-0.${base}:443
          route:
            enabled: true
EOF

oc -n "$NS" wait --for=condition=Ready pod -l app=jumpstarter-controller --timeout=300s

mkdir -p "$RUNBOOK_DIR"
jmp admin create client crc-caib --namespace "$NS" --insecure-tls --save --nointeractive
cp "$HOME/.config/jumpstarter/clients/crc-caib.yaml" "$RUNBOOK_DIR/crc-caib.client.yaml"
chmod 600 "$RUNBOOK_DIR/crc-caib.client.yaml"

oc -n "$NS" delete exporter "$EXPORTER_NAME" --ignore-not-found
jmp admin create exporter "$EXPORTER_NAME" \
  --namespace "$NS" --insecure-tls --save --nointeractive \
  --label runbook=manual-qemu --label target=qemu \
  --label board-type=virtual --label enabled=true

RUNBOOK_DIR="$RUNBOOK_DIR" python3 - <<'PY'
import os, pathlib, re, sys
stub = pathlib.Path.home() / ".config/jumpstarter/exporters/manual-qemu-aarch64.yaml"
out = pathlib.Path(os.environ["RUNBOOK_DIR"]) / "config.yaml"
text = stub.read_text()
def must(pat, desc):
    m = re.search(pat, text, re.MULTILINE)
    if not m:
        sys.exit(f"Missing {desc}")
    return m.group(1).strip()
endpoint, token = must(r'^endpoint:\s*(.+)$', 'endpoint'), must(r'^token:\s*(.+)$', 'token')
ca = re.search(r'^\s+ca:\s*(.+)$', text, re.MULTILINE)
tls = f"  ca: {ca.group(1).strip()}\n  insecure: true" if ca else "  insecure: true"
out.write_text(f"""apiVersion: jumpstarter.dev/v1alpha1
kind: ExporterConfig
metadata:
  namespace: jumpstarter-lab
  name: manual-qemu-aarch64
endpoint: {endpoint}
tls:
{tls}
token: {token}
export:
  serial:
    ref: qemu.console
  storage:
    ref: qemu.flasher
  power:
    ref: qemu.power
  qemu:
    type: jumpstarter_driver_qemu.driver.Qemu
    config:
      smp: 2
      mem: "2G"
      default_partitions:
        OVMF_CODE.fd: /usr/share/AAVMF/AAVMF_CODE.fd
        OVMF_VARS.fd: /usr/share/AAVMF/AAVMF_VARS.fd
      hostfwd:
        ssh:
          hostport: 9022
          guestport: 22
""")
print(out)
PY
chmod 600 "$RUNBOOK_DIR/config.yaml"

oc adm policy add-scc-to-user privileged -z default -n "$NS"
oc -n "$NS" delete pod "$EXPORTER_NAME" secret "${EXPORTER_NAME}-config" --ignore-not-found
oc -n "$NS" create secret generic "${EXPORTER_NAME}-config" \
  --from-file=config.yaml="$RUNBOOK_DIR/config.yaml"

cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: ${EXPORTER_NAME}
  namespace: ${NS}
  labels:
    app: jumpstarter-qemu-exporter
    runbook: manual-qemu
spec:
  restartPolicy: Always
  containers:
    - name: exporter
      image: ${QEMU_IMAGE}
      command: ["/jumpstarter/bin/jmp", "run", "--exporter-config", "/etc/jumpstarter/exporters/config.yaml"]
      securityContext:
        privileged: true
      resources:
        requests:
          cpu: "1"
          memory: 2Gi
        limits:
          cpu: "2"
          memory: 4Gi
      volumeMounts:
        - name: exporter-config
          mountPath: /etc/jumpstarter/exporters
          readOnly: true
        - name: qemu-data
          mountPath: /var/lib/jumpstarter
  volumes:
    - name: exporter-config
      secret:
        secretName: ${EXPORTER_NAME}-config
    - name: qemu-data
      emptyDir:
        sizeLimit: 12Gi
EOF

oc -n "$NS" wait --for=condition=Ready "pod/${EXPORTER_NAME}" --timeout=5m

oc patch operatorconfig config -n automotive-dev-operator-system --type=merge -p '
spec:
  jumpstarter:
    namespace: jumpstarter-lab
    image: quay.io/jumpstarter-dev/jumpstarter:0.9.0
    defaultLeaseDuration: "01:00:00"
    targetMappings:
      qemu:
        selector: "runbook=manual-qemu,target=qemu"
        flashCmd: "j storage flash oci://{image_uri}"
'

echo "Jumpstarter baseDomain: $base"
echo "Client config: $RUNBOOK_DIR/crc-caib.client.yaml"
echo "Next: wait for exporter Available, run the power smoke, then caib image build-dev --flash"
oc -n "$NS" get exporter "$EXPORTER_NAME"
```

The script does not apply the CoreDNS workaround. If `getent` inside a pod fails, do section 7 by hand before the flash.
