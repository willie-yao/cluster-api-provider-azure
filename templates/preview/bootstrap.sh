#!/usr/bin/env bash
# Bootstrap script run inside the Bicep deploymentScripts ACI container.
# Connects to the AKS mgmt cluster, installs CAPI Operator (with CAPZ +
# Helm addon), applies the alpha-preview workload cluster, optionally
# patches NRMS NSG rules in internal subscriptions, and writes the
# workload cluster's kubeconfig back to Key Vault.

set -o errexit
set -o nounset
set -o pipefail

# Inputs (passed in via Bicep environmentVariables):
#   AKS_NAME, AKS_RG, AKS_NODE_RG
#   UAMI_CLIENT_ID
#   KV_NAME
#   AZURE_LOCATION, AZURE_SUBSCRIPTION_ID, AZURE_TENANT_ID
#   CLUSTER_NAME, CLUSTER_NAMESPACE, CI_VERSION
#   K8S_FEATURE_GATES (optional, defaults to AllAlpha=true,AllBeta=true)
#   K8S_RUNTIME_CONFIG (optional, defaults to api/all=true)
#   CONTROL_PLANE_MACHINE_COUNT, WORKER_MACHINE_COUNT
#   NETWORKING_MODE        (public | internal)
#   AZURE_VNET_CIDR, AZURE_CP_SUBNET_CIDR, AZURE_NODE_SUBNET_CIDR  (internal only)
#   AZURE_INTERNAL_LB_PRIVATE_IP                                   (internal only)
#   APISERVER_LB_DNS_SUFFIX                                        (internal only)
#   EXTRA_FEATURE_RECIPES  (space-separated list of files in features/)

# Pinned component versions. Bump together in CAPZ release PRs.
# Phase 0 verification matrix (validated 2026-05-21):
#   - operator v0.27.0 is the first line that accepts v1beta2 provider release
#     contracts (v0.21 hard-rejects). See operator phases.go::validateRepoCAPIVersion.
#   - CAPI v1.13.2 ships v1beta2 metadata; serves v1beta1 via conversion webhook.
#   - CAPZ v1.24.1 is the matching CAPZ release for CAPI v1.13.x and includes
#     the APIServerILB feature gate (added in v1.18.0).
#   - helm-addon v0.6.4 matches the modern CAPI line.
CAPI_VERSION="${CAPI_VERSION:-v1.13.2}"
CAPZ_VERSION="${CAPZ_VERSION:-v1.24.1}"
CAPI_OPERATOR_VERSION="${CAPI_OPERATOR_VERSION:-v0.27.0}"
HELM_ADDON_VERSION="${HELM_ADDON_VERSION:-v0.6.4}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.15.3}"

# az_retry and wait_and_fix_nsg_rules are sourced from scripts/peer-vnets.sh
# inside fetch_preview_tree (after the repo is cloned). That script has a
# bottom-of-file sourcing guard so its main() does not run on source.

# Wait for a Kubernetes resource to exist before kubectl waits on its status.
# kubectl wait fails immediately if the object isn't there yet, which is
# common during CAPI Operator install when CRDs are still being applied.
wait_for_object() {
    local kind="$1" name="$2" ns="${3:-default}" timeout="${4:-300}"
    local deadline=$(( $(date +%s) + timeout ))
    while ! kubectl get "${kind}" "${name}" -n "${ns}" >/dev/null 2>&1; do
        if (( $(date +%s) > deadline )); then
            echo "timed out waiting for ${kind}/${name} in ns ${ns}" >&2
            return 1
        fi
        sleep 5
    done
}

# Install a missing CLI tool from the Alpine package repo (the
# mcr.microsoft.com/azure-cli image is Alpine-based). Used to add tools the
# image doesn't bundle (e.g. git, helm) without rebuilding a custom image.
# Progress goes to stderr so callers that capture stdout (e.g.
# ensure_ssh_keypair) don't pull "installing X via apk" into return values.
ensure_tool() {
    local tool="$1"
    if command -v "${tool}" >/dev/null 2>&1; then
        return 0
    fi
    echo "  installing ${tool} via apk" >&2
    apk add --no-cache --quiet "${tool}" >&2
}

# Generate or fetch a stable SSH keypair for the workload cluster nodes and
# emit the base64-encoded public key on stdout. Persisting in Key Vault keeps
# the value stable across re-runs of the bootstrap script so AzureMachineTemplate's
# immutable spec.template.spec.sshPublicKey doesn't change between deploys.
ensure_ssh_keypair() {
    ensure_tool openssh-keygen
    local secret_name="workload-ssh-public-key-b64"
    local cached
    cached=$(az keyvault secret show --vault-name "${KV_NAME}" --name "${secret_name}" \
        --query value -o tsv 2>/dev/null || true)
    if [[ -n "${cached}" ]]; then
        echo "${cached}"
        return 0
    fi
    local tmpdir; tmpdir=$(mktemp -d)
    ssh-keygen -t rsa -b 2048 -N "" -f "${tmpdir}/id_rsa" -C "capz-preview" >/dev/null
    local b64
    b64=$(base64 -w 0 < "${tmpdir}/id_rsa.pub")
    az keyvault secret set --vault-name "${KV_NAME}" --name "${secret_name}" \
        --value "${b64}" --only-show-errors >/dev/null
    az keyvault secret set --vault-name "${KV_NAME}" --name "workload-ssh-private-key" \
        --file "${tmpdir}/id_rsa" --only-show-errors >/dev/null
    rm -rf "${tmpdir}"
    echo "${b64}"
}

main() {
    echo "================================================================"
    echo "CAPZ alpha-k8s preview bootstrap"
    echo "  cluster:      ${CLUSTER_NAME}"
    echo "  ci version:   ${CI_VERSION}"
    echo "  mode:         ${NETWORKING_MODE}"
    echo "  location:     ${AZURE_LOCATION}"
    echo "================================================================"

    fetch_preview_tree
    connect_to_aks
    install_cert_manager
    install_capi_operator
    annotate_capz_workload_identity
    apply_workload_cluster
    apply_feature_recipes
    if [[ "${NETWORKING_MODE}" == "internal" ]]; then
        fix_nrms_nsg_rules
    fi
    wait_for_workload_cluster_ready
    publish_kubeconfig

    echo "bootstrap complete. retrieve the workload kubeconfig with:"
    echo "  az keyvault secret show --vault-name ${KV_NAME} \\"
    echo "    --name workload-kubeconfig --query value -o tsv > workload.kubeconfig"
}

# Bicep inlines this script via loadTextContent(), but the kustomize tree
# and feature recipes are not inlined. Clone the CAPZ repo at the pinned
# ref so paths like overlays/<mode> and features/<recipe>.yaml resolve.
fetch_preview_tree() {
    local repo_url="${PREVIEW_REPO_URL:-https://github.com/kubernetes-sigs/cluster-api-provider-azure.git}"
    local repo_ref="${PREVIEW_REPO_REF:-main}"
    echo "--- fetching preview tree from ${repo_url}@${repo_ref} ---"

    # The Azure deploymentScript azureCLI image at 2.61.0 ships az + a stripped
    # Alpine base. It does NOT bundle git, helm, curl, or kubectl. Install the
    # full toolchain up-front from Alpine's package repo so subsequent steps
    # don't need to retry one tool at a time.
    ensure_tool git
    ensure_tool helm
    ensure_tool curl
    ensure_tool kubectl
    rm -rf /tmp/capz
    git clone --depth=1 --branch "${repo_ref}" "${repo_url}" /tmp/capz
    cd /tmp/capz/templates/preview

    # Reuse az_retry, wait_and_fix_nsg_rules, peer_vnets, etc. from the
    # canonical peer-vnets.sh. The bottom-of-file `BASH_SOURCE == $0`
    # guard prevents its main() from running when sourced.
    # shellcheck disable=SC1091
    source /tmp/capz/scripts/peer-vnets.sh

    install_kustomize
    install_clusterctl
}

install_kustomize() {
    if command -v kustomize >/dev/null 2>&1; then
        return 0
    fi
    echo "  installing kustomize"
    curl -sSL -o /tmp/install_kustomize.sh \
        "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
    bash /tmp/install_kustomize.sh 5.4.3 /usr/local/bin
}

# clusterctl is used for variable substitution into the rendered manifests.
# envsubst is the wrong tool: it does not understand the `${VAR:=default}`
# syntax (leaves it as a literal) and worse, it mangles `$${VAR}` (which
# our embedded cloud-init scripts use to defer expansion until node boot).
install_clusterctl() {
    if command -v clusterctl >/dev/null 2>&1; then
        return 0
    fi
    echo "  installing clusterctl ${CAPI_VERSION}"
    curl -sSL -o /usr/local/bin/clusterctl \
        "https://github.com/kubernetes-sigs/cluster-api/releases/download/${CAPI_VERSION}/clusterctl-linux-amd64"
    chmod +x /usr/local/bin/clusterctl
}

connect_to_aks() {
    echo "--- fetching AKS credentials ---"
    az_retry 5 az aks get-credentials \
        --resource-group "${AKS_RG}" \
        --name "${AKS_NAME}" \
        --overwrite-existing --only-show-errors
}

install_cert_manager() {
    echo "--- installing cert-manager ${CERT_MANAGER_VERSION} ---"
    helm repo add jetstack https://charts.jetstack.io --force-update
    helm repo update
    helm upgrade --install cert-manager jetstack/cert-manager \
        --namespace cert-manager --create-namespace \
        --version "${CERT_MANAGER_VERSION}" \
        --set crds.enabled=true \
        --wait --timeout 5m
}

install_capi_operator() {
    echo "--- installing CAPI Operator ${CAPI_OPERATOR_VERSION} (CAPZ=${CAPZ_VERSION}) ---"
    helm repo add capi-operator https://kubernetes-sigs.github.io/cluster-api-operator
    helm repo update

    # cluster-api-operator v0.21+ requires provider name as the YAML key with
    # an optional `version` field. The pre-v0.20 string shorthand
    # ("cluster-api:vX.Y.Z") is rejected by the chart's JSON schema.
    # bootstrap+controlPlane (kubeadm) are required because the workload
    # cluster manifests reference KubeadmControlPlane and KubeadmConfigTemplate.
    cat > /tmp/capi-operator-values.yaml <<EOF
core:
  cluster-api:
    version: ${CAPI_VERSION}
    manager:
      featureGates:
        ClusterTopology: true
        MachinePool: true
bootstrap:
  kubeadm:
    version: ${CAPI_VERSION}
controlPlane:
  kubeadm:
    version: ${CAPI_VERSION}
infrastructure:
  azure:
    namespace: capz-system
    version: ${CAPZ_VERSION}
    configSecret:
      name: azure-variables
      namespace: capz-system
    manager:
      featureGates:
        # APIServerILB is alpha in CAPZ and required for the internal
        # overlay's apiServerLB.frontendIPs (public + private) to work.
        # Added in CAPZ v1.18.0; CAPZ_VERSION must be >= v1.18.0.
        APIServerILB: true
        MachinePool: true
addon:
  helm:
    version: ${HELM_ADDON_VERSION}
EOF

    # Pre-create the configSecret namespace + an empty secret that CAPZ
    # reads identity material from. We use Workload Identity so the secret
    # itself is empty; the UAMI client ID is wired in via SA annotation.
    # The CAPI Operator chart manages the capz-system namespace + the
    # InfrastructureProvider CR. We create the configSecret AFTER the helm
    # install so that:
    #  - helm doesn't try to adopt/overwrite the namespace mid-flight, and
    #  - the InfraProvider's secret lookup happens against a stable cluster.
    # The operator will retry the InfraProvider reconcile until the secret
    # appears, so create-after-helm is safe.
    helm upgrade --install capi-operator capi-operator/cluster-api-operator \
        --namespace capi-operator-system --create-namespace \
        --version "${CAPI_OPERATOR_VERSION}" \
        -f /tmp/capi-operator-values.yaml \
        --wait --timeout 15m

    echo "--- creating azure-variables configSecret in capz-system ---"
    # The helm chart already created the namespace (createNamespace defaults true).
    # Just create the secret CAPZ + ASO read identity config from. Workload
    # Identity means there is no client secret — the UAMI client ID is wired
    # in via SA annotation in a later step.
    kubectl create namespace capz-system --dry-run=client -o yaml | kubectl apply -f -
    kubectl create secret generic azure-variables -n capz-system \
        --from-literal=AZURE_CLIENT_ID="${UAMI_CLIENT_ID}" \
        --from-literal=AZURE_TENANT_ID="${AZURE_TENANT_ID}" \
        --from-literal=AZURE_SUBSCRIPTION_ID="${AZURE_SUBSCRIPTION_ID}" \
        --dry-run=client -o yaml | kubectl apply -f -

    echo "--- waiting for CAPI + CAPZ controllers to become Available ---"
    wait_for_object deployment capi-controller-manager capi-system 600
    kubectl -n capi-system wait --for=condition=Available \
        deployment/capi-controller-manager --timeout=10m
    wait_for_object deployment capz-controller-manager capz-system 600
    kubectl -n capz-system wait --for=condition=Available \
        deployment/capz-controller-manager --timeout=10m
}

annotate_capz_workload_identity() {
    echo "--- annotating capz-manager + ASO SAs for Workload Identity ---"
    kubectl -n capz-system annotate serviceaccount capz-manager \
        "azure.workload.identity/client-id=${UAMI_CLIENT_ID}" --overwrite

    # ASO ships alongside CAPZ in modern versions; annotate it too so its
    # reconciles use Workload Identity when the user picks an ASO-backed
    # feature recipe. Safe to be a no-op if the SA doesn't exist.
    if kubectl -n capz-system get serviceaccount azureserviceoperator-default >/dev/null 2>&1; then
        kubectl -n capz-system annotate serviceaccount azureserviceoperator-default \
            "azure.workload.identity/client-id=${UAMI_CLIENT_ID}" --overwrite
        kubectl -n capz-system label serviceaccount azureserviceoperator-default \
            "azure.workload.identity/use=true" --overwrite
    fi

    kubectl -n capz-system rollout restart deployment/capz-controller-manager
    kubectl -n capz-system rollout status  deployment/capz-controller-manager --timeout=5m
}

apply_workload_cluster() {
    echo "--- rendering and applying workload cluster (overlay=${NETWORKING_MODE}) ---"

    # The Bicep deploymentScript supportingScriptUris dumps the entire
    # templates/preview tree into $PWD. Pick the overlay for this mode.
    local overlay="overlays/${NETWORKING_MODE}"
    if [[ ! -d "${overlay}" ]]; then
        echo "overlay directory '${overlay}' not found" >&2
        exit 1
    fi

    export CLUSTER_NAME CLUSTER_IDENTITY_NAME="${CLUSTER_NAME}-identity"
    export AZURE_LOCATION AZURE_SUBSCRIPTION_ID AZURE_TENANT_ID
    export AZURE_CLIENT_ID="${UAMI_CLIENT_ID}"
    export AZURE_RESOURCE_GROUP="${AKS_RG}"
    # KCP and MachineDeployment .spec.version (the field CAPI validates) must
    # be a bare semver like "v1.37.0-alpha.0". CAPI v1.13's defaulter
    # auto-prepends "v" to any version not starting with "v", which corrupts
    # the historical "ci/v1.X.Y-foo" sentinel into "vci/v1.X.Y-foo" and the
    # semver validator then rejects it. The "ci/" prefix still belongs in the
    # kubeadm clusterConfiguration.kubernetesVersion (set by
    # base/ci-version-patch.yaml) where kubeadm uses it to resolve images
    # against dl.k8s.io/ci/<ver>.
    export KUBERNETES_VERSION="${CI_VERSION}"
    export CI_VERSION
    # AzureMachineTemplate spec.template.spec is immutable. To keep the
    # script idempotent across re-runs we persist a generated SSH key in Key
    # Vault and reuse it. The base manifests interpolate ${AZURE_SSH_PUBLIC_KEY_B64:=""}
    # into AzureMachineTemplate.spec.template.spec.sshPublicKey; an empty
    # value triggers CAPZ's defaulter to generate a random key on first apply,
    # then later applies (with empty) fail the immutability webhook.
    export AZURE_SSH_PUBLIC_KEY_B64="$(ensure_ssh_keypair)"
    export K8S_FEATURE_GATES="${K8S_FEATURE_GATES:-AllAlpha=true,AllBeta=true}"
    export K8S_RUNTIME_CONFIG="${K8S_RUNTIME_CONFIG:-api/all=true}"
    export CONTROL_PLANE_MACHINE_COUNT="${CONTROL_PLANE_MACHINE_COUNT:-1}"
    export WORKER_MACHINE_COUNT="${WORKER_MACHINE_COUNT:-2}"
    # Internal-only vars; harmless if unused by the public overlay.
    export AZURE_VNET_CIDR="${AZURE_VNET_CIDR:-}"
    export AZURE_CP_SUBNET_CIDR="${AZURE_CP_SUBNET_CIDR:-}"
    export AZURE_NODE_SUBNET_CIDR="${AZURE_NODE_SUBNET_CIDR:-}"
    export AZURE_INTERNAL_LB_PRIVATE_IP="${AZURE_INTERNAL_LB_PRIVATE_IP:-}"
    export APISERVER_LB_DNS_SUFFIX="${APISERVER_LB_DNS_SUFFIX:-apiserver}"

    kustomize build "${overlay}" > /tmp/cluster-rendered.yaml
    # Use clusterctl (not envsubst) for variable substitution: clusterctl
    # understands ${VAR:=default} and correctly converts $${VAR} (escape)
    # back to a literal ${VAR} for runtime bash to expand on the nodes.
    clusterctl generate yaml --from /tmp/cluster-rendered.yaml | kubectl apply -f -
}

apply_feature_recipes() {
    local recipes="${EXTRA_FEATURE_RECIPES:-}"
    if [[ -z "${recipes}" ]]; then
        return 0
    fi
    echo "--- applying feature recipes: ${recipes} ---"
    for recipe in ${recipes}; do
        local file="features/${recipe}.yaml"
        if [[ ! -f "${file}" ]]; then
            echo "feature recipe '${recipe}' not found at ${file}, skipping" >&2
            continue
        fi
        # Feature recipes are JSON-patches against the KCP — apply with
        # kubectl patch, not kubectl apply.
        kubectl patch kubeadmcontrolplane "${CLUSTER_NAME}-control-plane" \
            --type json --patch-file "${file}"
    done
}

# Patches NRMS-Rule-101 (TCP) and NRMS-Rule-103 (UDP) deny rules that
# MSFT-internal Azure subscriptions auto-apply to every NSG. Delegates to
# peer-vnets.sh::wait_and_fix_nsg_rules; we just translate our env-var
# names to that function's contract.
fix_nrms_nsg_rules() {
    # peer-vnets.sh iterates over three RGs: (node, mgmt, workload-as-RG).
    # In tilt+aks-as-mgmt the workload cluster has its own RG named after
    # CLUSTER_NAME. In our flow workload+mgmt share AKS_RG, so we override
    # CLUSTER_NAME for the function call so the third iteration becomes an
    # idempotent duplicate of the second instead of timing out on a
    # non-existent RG.
    AKS_NODE_RESOURCE_GROUP="${AKS_NODE_RG}" \
    AKS_RESOURCE_GROUP="${AKS_RG}" \
    CLUSTER_NAME="${AKS_RG}" \
    wait_and_fix_nsg_rules
}

wait_for_workload_cluster_ready() {
    echo "--- waiting for workload cluster to be Available (up to 30m) ---"
    # CAPI v1beta2 replaced the legacy `Ready` cluster condition with
    # `Available`. Older code used --for=condition=Ready which never
    # resolves on v1.13.x+.
    kubectl wait cluster "${CLUSTER_NAME}" \
        -n "${CLUSTER_NAMESPACE:-default}" \
        --for=condition=Available --timeout=30m
}

publish_kubeconfig() {
    echo "--- exporting workload kubeconfig to Key Vault ${KV_NAME} ---"

    # clusterctl ships in the CAPI Operator container, but the deployment
    # script runs the azure-cli image. Use the secret CAPI publishes
    # natively instead of installing clusterctl just for this.
    kubectl get secret -n "${CLUSTER_NAMESPACE:-default}" \
        "${CLUSTER_NAME}-kubeconfig" \
        -o jsonpath='{.data.value}' | base64 -d > /tmp/workload.kubeconfig

    az_retry 5 az keyvault secret set \
        --vault-name "${KV_NAME}" \
        --name workload-kubeconfig \
        --file /tmp/workload.kubeconfig \
        --only-show-errors --output none

    rm -f /tmp/workload.kubeconfig
}

main "$@"
