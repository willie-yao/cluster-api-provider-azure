# CAPZ Alpha Kubernetes Preview

> **Preview / not for production.** This template provisions an AKS
> management cluster running the very latest CAPZ alongside a workload
> cluster that downloads alpha Kubernetes binaries at boot. Use it to
> kick the tires on alpha features. Do not run real workloads on it.

[![Deploy to Azure](https://aka.ms/deploytoazurebutton)](https://portal.azure.com/#create/Microsoft.Template/uri/https%3A%2F%2Fraw.githubusercontent.com%2Fwillie-yao%2Fcluster-api-provider-azure%2Falpha-features%2Ftemplates%2Fpreview%2Fmain.json)

One-click deploy from the Azure Portal. Gives you a Kubernetes workload
cluster pinned to a `dl.k8s.io/ci/<ver>` build with `AllAlpha=true`,
`AllBeta=true`, and `runtime-config=api/all=true` so every alpha feature
gate and alpha API is on by default.

## What it deploys

| Resource | Purpose |
| --- | --- |
| **AKS management cluster** (`Standard_D2s_v3` × 2) | Hosts CAPI Operator + CAPZ. Override `aksNodeVmSize` to a burstable SKU (e.g. `Standard_B2s`) on subs that allow it. |
| **User-assigned managed identity** + role assignment | CAPZ auth (Workload Identity, no secrets). |
| **Federated credentials** | Wire AKS OIDC issuer to `capz-manager` and ASO. |
| **Deployment script** (one-shot ACI) | Installs cert-manager, CAPI Operator (CAPZ + Helm addon), applies the workload cluster. |
| **Workload Cluster** (`Standard_D2s_v3` × 1 CP + 2 workers) | The alpha-Kubernetes cluster you came for. |

Everything lands in a single resource group. Cleanup is one command.

## Prerequisites

The deploy itself needs **Contributor + User Access Administrator** (or
`Owner`) on the target resource group so the template can create the
AKS cluster, UAMI, and the role assignments wiring them together.

After deploy, you'll pull the workload cluster's kubeconfig out of a
Secret on the AKS management cluster. That requires AKS admin
credentials, which `az aks get-credentials --admin` returns to anyone
with `Contributor` (or `Azure Kubernetes Service Cluster Admin Role`)
on the AKS resource. Sub-scoped `Contributor`/`Owner` covers this by
inheritance.

## Quick start

1. Create a resource group:

   ```bash
   # Pick a region where the CAPZ community gallery (capi-ubun2-2404) is
   # replicated.
   az group create -n capz-preview-rg -l westus2
   ```

2. Deploy the template:

   ```bash
   az deployment group create \
     -g capz-preview-rg \
     -n capz-preview \
     -f main.bicep
   ```

   Or click the **Deploy to Azure** button above. Pick the resource
   group in the portal form and hit *Review + create*.

3. Wait 15–30 minutes (most of it is the workload cluster control plane
   coming up on alpha binaries).

4. Pull the workload kubeconfig out of the management cluster:

   ```bash
   AKS_NAME=$(az deployment group show -g capz-preview-rg -n capz-preview \
                --query properties.outputs.aksClusterName.value -o tsv)
   az aks get-credentials -g capz-preview-rg -n "$AKS_NAME" --admin --overwrite-existing
   kubectl get secret capz-preview-kubeconfig \
     -o jsonpath='{.data.value}' | base64 -d > workload.kubeconfig
   kubectl --kubeconfig workload.kubeconfig get nodes
   ```

   Your kubectl context after step 4 is the **management cluster**.
   `kubectl get cluster,azurecluster,machine` shows the CAPI resources
   that produced the workload cluster.

5. Tear it all down:

   ```bash
   az group delete -n capz-preview-rg --yes --no-wait
   ```

## Parameters

| Parameter | Default | Notes |
| --- | --- | --- |
| `ciVersion` | `v1.37.0-alpha.0` | Latest named alpha minor at deploy time. The alpha stream lives on the next-not-yet-stable minor; bump as new alphas are cut. Supports rolling CI builds too (e.g. `v1.37.0-alpha.0.1028+5edef25b704d23` from `latest-1.37`). |
| `featureGatesOverride` | _(empty)_ | Override the default `AllAlpha=true,AllBeta=true`. Useful when a specific alpha is known to break bringup. |
| `extraFeatureRecipes` | _(empty)_ | Space-separated list of opt-in recipes from `features/`. Currently: `dra`. |
| `controlPlaneMachineCount` | `1` | Replica count for the workload cluster control plane. Auto-bumped to `3` in internal mode for apiserver-ILB hairpin routing. |
| `workerMachineCount` | `2` | Workers are alpha binaries too. |
| `aksKubernetesVersion` | `1.33.2` | The **management** cluster k8s version. Has nothing to do with the alpha workload cluster. |
| `previewRepoUrl` / `previewRepoRef` | `willie-yao/cluster-api-provider-azure` / `alpha-features` | The deploymentScript clones this to find `kustomize` overlays and feature recipes. Defaults point at the preview fork until the template lands upstream. Pin to a commit SHA for reproducible deploys. |

## Which alpha features "just work"?

Any feature whose entire bringup is enabled by a `--feature-gates` flag
(and optionally `--runtime-config`) works out of the box, because the
template turns on every gate and every alpha API.

Examples in current/recent k8s alphas that work with no extra setup:

- `InPlacePodVerticalScaling`
- `PodLevelResources`
- `CustomCPUCFSQuotaPeriod`
- `JobSuccessPolicy`
- `MutatingAdmissionPolicy`
- Any new alpha API group (turned on via `api/all=true`)

Features that need **more than gates** — and therefore an explicit recipe:

| Feature | Why a recipe is needed | Recipe |
| --- | --- | --- |
| Dynamic Resource Allocation (DRA) | Needs containerd CDI plugin enabled on every node; restricts `runtime-config` to specific `resource.k8s.io` versions. | `features/dra.yaml` |
| Node swap | Needs swap configured at the OS layer (cloud-init), not just kubelet gates. | TBD |
| User namespaces | Kernel-version-dependent userns_remap setup. | TBD |

Apply a recipe by passing `extraFeatureRecipes=dra` at deploy time, or
patch the running cluster manually:

```bash
kubectl --kubeconfig <mgmt-kubeconfig> patch kubeadmcontrolplane capz-preview-control-plane \
  --type json --patch-file templates/preview/features/dra.yaml
```

## Cleanup

Tear it down when you're done:

```bash
az group delete -n capz-preview-rg --yes --no-wait
```

That removes the management RG, the AKS-managed node RG, and everything
in them.

## How it works

1. **Bicep** creates the ARM resources (AKS, UAMI + federated cred) and
   one `deploymentScript` resource that runs `bootstrap.sh` in an
   Azure-CLI ACI container with the UAMI attached.
2. **`bootstrap.sh`** clones this repo, runs `az aks get-credentials`,
   installs cert-manager + CAPI Operator (with CAPZ + Helm addon
   providers), annotates the `capz-manager` SA with the UAMI client ID
   for Workload Identity, then `kustomize build` + `clusterctl generate
   yaml --from -` + `kubectl apply` the workload cluster spec.
3. **CAPZ** reconciles the `Cluster` + `AzureCluster` + `KubeadmControlPlane`
   + `MachineDeployment` resources, calling Azure to create the workload
   cluster's NSGs, LBs, public IPs, and VMs. The VMs run `preKubeadmCommands`
   that download `kubeadm`/`kubelet`/`kubectl` from `dl.k8s.io/ci/<ver>`
   and the corresponding control-plane container images from
   `gcr.io/k8s-staging-ci-images/`.
4. When the workload `Cluster` becomes `Available`, CAPI publishes its
   kubeconfig as a Secret (`<cluster>-kubeconfig`) in the management
   cluster's `default` namespace. Step 4 of the quickstart reads that
   Secret with kubectl.

The `templates/preview/base/` kustomize layer holds the shared spec; the
`overlays/public` overlay composes the base into the rendered manifest
set applied to the management cluster.

## Updating the alpha version

When a newer Kubernetes alpha is published:

1. Check that the build exists under
   `https://dl.k8s.io/ci/<ver>/bin/linux/amd64/kubelet`.
2. Re-deploy with `-p ciVersion=<ver>` **and a fresh deployment name**.
   The deploymentScript only re-runs when ARM detects a change to its
   `forceUpdateTag`, which defaults to the deployment name. Using the
   same `-n` twice in the same RG is treated as an idempotent no-op and
   your new parameters are silently ignored.

   ```bash
   az deployment group create \
     -g capz-preview-rg \
     -n capz-preview-$(date +%s) \
     -f main.bicep \
     -p ciVersion=<ver>
   ```

   The existing AKS + UAMI stay; the deploymentScript re-runs and
   `kubectl apply`s an updated KCP, which rolls the control plane and
   node pools.

If a new alpha breaks bringup with the blanket `AllAlpha=true`, fall back
to listing only the gates you care about:

```bash
az deployment group create -g capz-preview-rg -n capz-preview-$(date +%s) \
  -f main.bicep \
  -p ciVersion=<ver> \
     featureGatesOverride='InPlacePodVerticalScaling=true,JobSuccessPolicy=true'
```

## Maintenance burden

By design this is _low_ maintenance:

- **No new VHDs.** Stock CAPI community-gallery Ubuntu image + boot-time
  binary download. Tracks upstream alphas automatically.
- **No per-feature plumbing for the 95%** that just need gates.
- **Manual recipes only for the ~5%** that need OS/containerd config.
  Add to `features/` as user demand surfaces them.
- **CAPZ + CAPI version bumps** are the only routine work — bump the
  pinned versions in `bootstrap.sh` when a new CAPZ ships.

## Limitations / known issues

- Linux amd64 only. No Windows, no arm64.
- Single region per deploy.
- Private cluster (private AKS API server) not enabled. Add via
  `--enable-private-cluster` in `main.bicep` if you need it.
- `AllAlpha=true` is a footgun on rare occasions — a half-baked alpha
  gate can break apiserver bringup. Workaround: `featureGatesOverride`.
- Workload cluster takes 15–30 min to come up because alpha images come
  from `gcr.io/k8s-staging-ci-images/`, which is slower than
  `registry.k8s.io`.
- `gcr.io/k8s-staging-ci-images` rotates old alphas — yesterday's alpha
  may not work tomorrow.
- Workload cluster's `cloud-controller-manager` Deployment is in
  `CrashLoopBackOff` (DefaultAzureCredential finds no credentials inside
  the workload cluster). `cloud-node-manager` works fine, so nodes
  come up Ready; the only functional impact is no
  `Service type=LoadBalancer` provisioning. Tracked for the next pass.
