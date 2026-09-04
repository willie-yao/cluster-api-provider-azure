#!/usr/bin/env bash

# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPSTREAM_REPO="kubernetes-sigs/cluster-api-provider-azure"
PROMOTION_REPO="kubernetes/k8s.io"
TESTGRID_SUMMARY_URL="https://testgrid.k8s.io/sig-cluster-lifecycle-cluster-api-provider-azure/summary"
TESTGRID_JOB="post-cluster-api-provider-azure-push-images"
CAPI_CLOUDBUILD_URL="https://raw.githubusercontent.com/kubernetes-sigs/cluster-api/main/cloudbuild.yaml"
STAGING_MANIFEST_URL="https://gcr.io/v2/k8s-staging-cluster-api-azure/cluster-api-azure-controller/manifests"
PRODUCTION_MANIFEST_URL="https://registry.k8s.io/v2/cluster-api-azure/cluster-api-azure-controller/manifests"

STAGE=""
TAG=""
DRY_RUN=false
ACKNOWLEDGE_IMAGE_JOB_STATUS=false
ORIGIN_SLUG=""

usage() {
  cat <<'EOF'
Usage:
  hack/release.sh prepare --tag <vN.N.N> [--dry-run] [--acknowledge-image-job-status]
  hack/release.sh promote --tag <vN.N.N> [--dry-run]
  hack/release.sh publish --tag <vN.N.N> [--dry-run]
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '%s\n' "$*"
}

is_stable_tag() {
  [[ "${1:-}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

release_branch_for_tag() {
  local tag="${1:?tag is required}"
  is_stable_tag "${tag}" || return 1
  local version="${tag#v}"
  local major minor patch
  IFS=. read -r major minor patch <<<"${version}"
  if ((10#${patch} == 0)); then
    printf 'main\n'
  else
    printf 'release-%d.%d\n' "$((10#${major}))" "$((10#${minor}))"
  fi
}

semver_gt() {
  local left="${1#v}"
  local right="${2#v}"
  local left_major left_minor left_patch right_major right_minor right_patch
  is_stable_tag "v${left}" || return 2
  is_stable_tag "v${right}" || return 2
  IFS=. read -r left_major left_minor left_patch <<<"${left}"
  IFS=. read -r right_major right_minor right_patch <<<"${right}"
  if ((10#${left_major} != 10#${right_major})); then
    ((10#${left_major} > 10#${right_major}))
  elif ((10#${left_minor} != 10#${right_minor})); then
    ((10#${left_minor} > 10#${right_minor}))
  else
    ((10#${left_patch} > 10#${right_patch}))
  fi
}

is_publish_confirmation() {
  [[ "${2:-}" == "publish ${1:-}" ]]
}

body_contains_details_url() {
  local details_url="${1:?Details URL is required}"
  tr -d '\r' | grep -Fqx "${details_url}"
}

registry_digest_from_headers() {
  tr -d '\r' | awk '
    tolower($1) == "docker-content-digest:" {
      digest = $2
    }
    END {
      print digest
    }
  '
}

render_command() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
}

run_mutation() {
  if ${DRY_RUN}; then
    render_command "$@"
    return 0
  fi
  "$@"
}

push_with_github_credentials() {
  local -a push_args=(git push "$@")
  if ${DRY_RUN}; then
    render_command env \
      GIT_CONFIG_COUNT=1 \
      GIT_CONFIG_KEY_0=credential.https://github.com.helper \
      "GIT_CONFIG_VALUE_0=!gh auth git-credential" \
      "${push_args[@]}"
    return 0
  fi
  GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0=credential.https://github.com.helper \
    GIT_CONFIG_VALUE_0='!gh auth git-credential' \
    GH_TOKEN="${GITHUB_TOKEN}" \
    "${push_args[@]}"
}

parse_args() {
  [[ $# -gt 0 ]] || {
    usage >&2
    return 1
  }

  STAGE="$1"
  shift
  case "${STAGE}" in
    prepare | promote | publish) ;;
    *)
      usage >&2
      return 1
      ;;
  esac

  TAG=""
  DRY_RUN=false
  ACKNOWLEDGE_IMAGE_JOB_STATUS=false
  local tag_seen=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --tag)
        [[ $# -ge 2 ]] || die "--tag requires a value"
        ${tag_seen} && die "--tag may only be specified once"
        TAG="$2"
        tag_seen=true
        shift 2
        ;;
      --dry-run)
        ${DRY_RUN} && die "--dry-run may only be specified once"
        DRY_RUN=true
        shift
        ;;
      --acknowledge-image-job-status)
        [[ "${STAGE}" == "prepare" ]] || die "--acknowledge-image-job-status is only valid for prepare"
        ${ACKNOWLEDGE_IMAGE_JOB_STATUS} && die "--acknowledge-image-job-status may only be specified once"
        ACKNOWLEDGE_IMAGE_JOB_STATUS=true
        shift
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done

  [[ -n "${TAG}" ]] || die "--tag is required"
  is_stable_tag "${TAG}" || die "tag must be a stable version in the form vN.N.N"
}

require_commands() {
  local command_name
  for command_name in "$@"; do
    command -v "${command_name}" >/dev/null 2>&1 || die "required command not found: ${command_name}"
  done
}

load_github_token() {
  if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    GITHUB_TOKEN="$(gh auth token 2>/dev/null)" || die "set GITHUB_TOKEN or authenticate with gh"
  fi
  [[ -n "${GITHUB_TOKEN}" ]] || die "set GITHUB_TOKEN or authenticate with gh"
  GH_TOKEN="${GITHUB_TOKEN}"
  export GITHUB_TOKEN GH_TOKEN
}

remote_slug() {
  local url="${1:?remote URL is required}"
  local slug
  case "${url}" in
    https://github.com/*)
      slug="${url#https://github.com/}"
      ;;
    git@github.com:*)
      slug="${url#git@github.com:}"
      ;;
    ssh://git@github.com/*)
      slug="${url#ssh://git@github.com/}"
      ;;
    *)
      return 1
      ;;
  esac
  slug="${slug%.git}"
  [[ "${slug}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || return 1
  printf '%s\n' "${slug}"
}

github_resource_exists() {
  local endpoint="${1:?endpoint is required}"
  local output status
  set +o errexit
  output="$(gh api --include "${endpoint}" 2>&1)"
  status=$?
  set -o errexit
  if ((status == 0)); then
    return 0
  fi
  if grep -Eq 'HTTP/[0-9.]+ 404|HTTP 404|Not Found' <<<"${output}"; then
    return 1
  fi
  die "GitHub API request failed for ${endpoint}: ${output}"
}

first_cloudbuild_image() {
  awk '
    /^steps:/ {
      in_steps = 1
      next
    }
    in_steps && /^[[:space:]]*-[[:space:]]+name:/ {
      sub(/^[[:space:]]*-[[:space:]]+name:[[:space:]]*/, "")
      sub(/[[:space:]]+#.*$/, "")
      gsub(/^'\''|'\''$/, "")
      gsub(/^"|"$/, "")
      print
      exit
    }
  '
}

metadata_contracts() {
  local target_major="${1:?major is required}"
  local target_minor="${2:?minor is required}"
  awk -v target_major="${target_major}" -v target_minor="${target_minor}" '
    function emit() {
      if (seen && major == target_major && minor == target_minor) {
        print contract
      }
    }
    /^[[:space:]]*-[[:space:]]+major:/ {
      emit()
      seen = 1
      major = $3
      minor = ""
      contract = ""
      next
    }
    seen && /^[[:space:]]+minor:/ {
      minor = $2
      next
    }
    seen && /^[[:space:]]+contract:/ {
      contract = $2
      next
    }
    END {
      emit()
    }
  ' "${REPO_ROOT}/metadata.yaml"
}

registry_digest() {
  local manifest_url="${1:?manifest URL is required}"
  local tag="${2:?tag is required}"
  local headers digest
  headers="$(curl -fsSIL \
    -H 'Accept: application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' \
    "${manifest_url}/${tag}")" || die "registry manifest not found: ${manifest_url}/${tag}"
  digest="$(registry_digest_from_headers <<<"${headers}")"
  [[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || die "registry did not return a valid manifest digest for ${tag}"
  printf '%s\n' "${digest}"
}

promotion_pr_title() {
  printf 'Image promotion for cluster-api-azure %s\n' "${1:?tag is required}"
}

promotion_pr_json() {
  local title
  title="$(promotion_pr_title "${TAG}")"
  gh pr list \
    --repo "${PROMOTION_REPO}" \
    --state all \
    --search "${title} in:title" \
    --limit 100 \
    --json number,title,state,url,mergedAt |
    jq --arg title "${title}" '[.[] | select(.title == $title)] | sort_by(.number) | reverse'
}

prepare_pr_json() {
  local login="${1:?login is required}"
  local branch="${2:?branch is required}"
  gh pr list \
    --repo "${UPSTREAM_REPO}" \
    --state all \
    --head "${branch}" \
    --base main \
    --limit 100 \
    --json number,state,url,headRepositoryOwner |
    jq --arg login "${login}" '[.[] | select(.headRepositoryOwner.login == $login)] | sort_by(.number) | reverse'
}

details_url_from_changelog() {
  awk '
    /^## Details$/ {
      in_details = 1
      next
    }
    in_details && /^https:\/\/github\.com\/kubernetes-sigs\/cluster-api-provider-azure\/compare\/v[0-9]+\.[0-9]+\.[0-9]+\.\.\.v[0-9]+\.[0-9]+\.[0-9]+$/ {
      print
      exit
    }
    in_details && /^## / {
      exit
    }
  '
}

details_url_matches_tag() {
  local tag="${1:?tag is required}"
  local url="${2:?URL is required}"
  [[ "${url}" =~ ^https://github\.com/kubernetes-sigs/cluster-api-provider-azure/compare/v[0-9]+\.[0-9]+\.[0-9]+\.\.\.v[0-9]+\.[0-9]+\.[0-9]+$ ]] &&
    [[ "${url##*...}" == "${tag}" ]]
}

previous_tag_for_branch() {
  local branch="${1:?branch is required}"
  git tag --merged "upstream/${branch}" --list |
    awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/' |
    sort -V |
    tail -n 1
}

verify_changelog() {
  local changelog="${1:?changelog is required}"
  local previous_tag="${2:?previous tag is required}"
  local expected_url details_count url_count
  expected_url="https://github.com/${UPSTREAM_REPO}/compare/${previous_tag}...${TAG}"
  details_count="$(grep -c '^## Details$' "${changelog}" || true)"
  url_count="$(grep -Fxc "${expected_url}" "${changelog}" || true)"
  [[ "${details_count}" == "1" ]] || die "${changelog} must contain exactly one Details heading"
  [[ "${url_count}" == "1" ]] || die "${changelog} must contain exactly one expected Details URL: ${expected_url}"
  [[ "$(details_url_from_changelog <"${changelog}")" == "${expected_url}" ]] ||
    die "${changelog} has an invalid Details block"
}

print_prepare_checklist() {
  cat <<'EOF'
Manual prerequisites:
  - For minor or major releases, update root metadata.yaml and complete the milestone and test-infra changes.
  - After the release-notes PR opens, review categories and wording before merging it.
EOF
}

print_promote_checklist() {
  cat <<'EOF'
Manual follow-up:
  - Review the staging digest and get the k8s.io image promotion PR approved and merged.
EOF
}

print_publish_checklist() {
  cat <<'EOF'
Manual follow-up:
  - Announce the release.
  - For minor or major releases, update Netlify, security and Dependabot branches, E2E provider versions, test-infra jobs, and the roadmap as needed.
EOF
}

check_local_main() {
  [[ -z "$(git status --porcelain)" ]] || die "the worktree must be clean"
  [[ "$(git branch --show-current)" == "main" ]] || die "prepare must run from the local main branch"

  local upstream_main_sha
  if ${DRY_RUN}; then
    upstream_main_sha="$(git ls-remote upstream refs/heads/main | awk '{print $1}')"
    [[ -n "${upstream_main_sha}" ]] || die "unable to resolve upstream/main"
  else
    local release_branch
    release_branch="$(release_branch_for_tag "${TAG}")"
    git fetch upstream '+refs/heads/main:refs/remotes/upstream/main'
    if [[ "${release_branch}" != "main" ]]; then
      git fetch upstream "+refs/heads/${release_branch}:refs/remotes/upstream/${release_branch}"
    fi
    upstream_main_sha="$(git rev-parse upstream/main)"
  fi

  [[ "$(git rev-parse HEAD)" == "${upstream_main_sha}" ]] ||
    die "local main must exactly match the current upstream/main"
}

check_remotes() {
  local upstream_url origin_url upstream_slug origin_slug push_url push_slug push_urls
  upstream_url="$(git remote get-url upstream)" || die "an upstream remote is required"
  origin_url="$(git remote get-url origin)" || die "an origin fork remote is required"
  upstream_slug="$(remote_slug "${upstream_url}")" || die "unsupported upstream remote URL: ${upstream_url}"
  origin_slug="$(remote_slug "${origin_url}")" || die "unsupported origin remote URL: ${origin_url}"
  [[ "${upstream_slug}" == "${UPSTREAM_REPO}" ]] ||
    die "upstream must point to ${UPSTREAM_REPO}, found ${upstream_slug}"
  [[ "${origin_slug}" != "${UPSTREAM_REPO}" ]] || die "origin must point to a fork, not ${UPSTREAM_REPO}"
  [[ "${origin_slug#*/}" == "cluster-api-provider-azure" ]] ||
    die "origin must point to a cluster-api-provider-azure fork, found ${origin_slug}"
  push_urls="$(git remote get-url --push --all origin)" || die "origin must have a push destination"
  [[ -n "${push_urls}" ]] || die "origin must have a push destination"
  while IFS= read -r push_url; do
    push_slug="$(remote_slug "${push_url}")" || die "unsupported origin push URL: ${push_url}"
    [[ "${push_slug}" == "${origin_slug}" ]] ||
      die "origin push destination must be ${origin_slug}, found ${push_slug}"
  done <<<"${push_urls}"
  ORIGIN_SLUG="${origin_slug}"
}

check_testgrid() {
  local summary status
  summary="$(curl -fsSL "${TESTGRID_SUMMARY_URL}")" || die "unable to read the Testgrid summary"
  status="$(jq -er --arg job "${TESTGRID_JOB}" '.[$job].overall_status' <<<"${summary}")" ||
    die "Testgrid summary does not contain ${TESTGRID_JOB}"
  if [[ "${status}" != "PASSING" ]]; then
    if ${ACKNOWLEDGE_IMAGE_JOB_STATUS}; then
      info "Acknowledged ${TESTGRID_JOB} status: ${status}"
    else
      die "${TESTGRID_JOB} is ${status}; investigate it or rerun with --acknowledge-image-job-status"
    fi
  else
    info "${TESTGRID_JOB}: PASSING"
  fi
}

check_cloudbuild_image() {
  local branch capz_cloudbuild capz_image capi_image
  branch="$(release_branch_for_tag "${TAG}")"
  if [[ "${branch}" == "main" ]]; then
    capz_image="$(first_cloudbuild_image <"${REPO_ROOT}/cloudbuild.yaml")"
  else
    capz_cloudbuild="$(curl -fsSL "https://raw.githubusercontent.com/${UPSTREAM_REPO}/${branch}/cloudbuild.yaml")" ||
      die "unable to read cloudbuild.yaml from ${branch}"
    capz_image="$(first_cloudbuild_image <<<"${capz_cloudbuild}")"
  fi
  capi_image="$(curl -fsSL "${CAPI_CLOUDBUILD_URL}" | first_cloudbuild_image)" ||
    die "unable to read Cluster API cloudbuild.yaml"
  [[ -n "${capz_image}" && -n "${capi_image}" ]] || die "unable to determine the first cloudbuild step image"
  [[ "${capz_image}" == "${capi_image}" ]] ||
    die "cloudbuild image mismatch on ${branch}: CAPZ uses ${capz_image}, CAPI uses ${capi_image}; update ${branch} in a separate PR before releasing"
  info "cloudbuild image matches Cluster API: ${capz_image}"
}

check_metadata_for_minor_or_major() {
  local version major minor patch
  version="${TAG#v}"
  IFS=. read -r major minor patch <<<"${version}"
  ((10#${patch} == 0)) || return 0

  local contract
  local -a contracts=()
  while IFS= read -r contract; do
    contracts+=("${contract}")
  done < <(metadata_contracts "${major}" "${minor}")
  ((${#contracts[@]} == 1)) ||
    die "metadata.yaml must contain exactly one release series for v${major}.${minor}"
  [[ -n "${contracts[0]}" ]] || die "metadata.yaml has an empty contract for v${major}.${minor}"
  info "metadata.yaml maps v${major}.${minor} to contract ${contracts[0]}"
}

check_tag_and_release_absent() {
  [[ -z "$(git ls-remote --tags upstream "refs/tags/${TAG}" "refs/tags/${TAG}^{}")" ]] ||
    die "upstream tag ${TAG} already exists"
  if github_resource_exists "repos/${UPSTREAM_REPO}/releases/tags/${TAG}"; then
    die "GitHub release ${TAG} already exists"
  fi
}

prepare_release() {
  require_commands bash curl git gh jq make
  load_github_token
  cd "${REPO_ROOT}"
  print_prepare_checklist
  check_remotes

  local fork_owner branch existing_pr successful_pr state state_label url
  fork_owner="${ORIGIN_SLUG%%/*}"
  branch="release-notes-${TAG}"
  existing_pr="$(prepare_pr_json "${fork_owner}" "${branch}")"
  if (($(jq 'length' <<<"${existing_pr}") > 0)); then
    successful_pr="$(jq '[.[] | select(.state == "OPEN" or .state == "MERGED")][0] // empty' <<<"${existing_pr}")"
    if [[ -n "${successful_pr}" ]]; then
      state="$(jq -r '.state' <<<"${successful_pr}")"
      url="$(jq -r '.url' <<<"${successful_pr}")"
      state_label="$(tr '[:upper:]' '[:lower:]' <<<"${state}")"
      info "Release-notes PR already ${state_label}: ${url}"
      return 0
    fi
    url="$(jq -r '.[0].url' <<<"${existing_pr}")"
    die "release-notes PR ${url} is closed without merge; resolve it before retrying"
  fi
  check_local_main
  git show-ref --verify --quiet "refs/heads/${branch}" &&
    die "local branch ${branch} already exists"
  [[ -z "$(git ls-remote --heads origin "refs/heads/${branch}")" ]] ||
    die "origin branch ${branch} already exists without an open PR"
  check_testgrid
  check_cloudbuild_image
  check_metadata_for_minor_or_major
  check_tag_and_release_absent

  if ${DRY_RUN}; then
    render_command make release-notes "RELEASE_TAG=${TAG}"
    render_command git switch -c "${branch}"
    render_command git add "CHANGELOG/${TAG}.md"
    render_command git -c commit.gpgsign=false commit --signoff -m "Add release notes for ${TAG}"
    push_with_github_credentials -u origin "${branch}"
    render_command gh pr create --repo "${UPSTREAM_REPO}" --base main --head "${fork_owner}:${branch}" \
      --title "Add release notes for ${TAG}" --body $'/kind documentation\n\n```release-note\nNONE\n```'
    return 0
  fi

  make release-notes "RELEASE_TAG=${TAG}"
  local changelog previous_tag changes
  changelog="CHANGELOG/${TAG}.md"
  [[ -f "${changelog}" ]] || die "release-notes did not create ${changelog}"
  changes="$(git status --porcelain --untracked-files=all)"
  [[ "${changes}" == "?? ${changelog}" ]] ||
    die "release-note generation changed unexpected files: ${changes}"
  previous_tag="$(previous_tag_for_branch "$(release_branch_for_tag "${TAG}")")"
  [[ -n "${previous_tag}" ]] || die "unable to determine the previous stable tag"
  verify_changelog "${changelog}" "${previous_tag}"

  git switch -c "${branch}"
  git add -- "${changelog}"
  git -c commit.gpgsign=false commit --signoff -m "Add release notes for ${TAG}"
  push_with_github_credentials -u origin "${branch}"
  gh pr create \
    --repo "${UPSTREAM_REPO}" \
    --base main \
    --head "${fork_owner}:${branch}" \
    --title "Add release notes for ${TAG}" \
    --body $'/kind documentation\n\n```release-note\nNONE\n```'

  existing_pr="$(prepare_pr_json "${fork_owner}" "${branch}")"
  [[ "$(jq -r '.[0].state // empty' <<<"${existing_pr}")" == "OPEN" ]] ||
    die "release-notes branch was pushed, but no open PR was found"
  info "Release-notes PR: $(jq -r '.[0].url' <<<"${existing_pr}")"
}

check_upstream_tag() {
  github_resource_exists "repos/${UPSTREAM_REPO}/git/ref/tags/${TAG}" ||
    die "upstream tag ${TAG} does not exist"
}

draft_release_json() {
  gh release view "${TAG}" \
    --repo "${UPSTREAM_REPO}" \
    --json tagName,isDraft,isPrerelease,publishedAt,body,assets
}

validate_release_identity() {
  local release_json="${1:?release JSON is required}"
  [[ "$(jq -r '.tagName' <<<"${release_json}")" == "${TAG}" ]] || die "release tag does not match ${TAG}"
  [[ "$(jq -r '.isPrerelease' <<<"${release_json}")" == "false" ]] || die "${TAG} must not be a prerelease"
}

promote_release() {
  require_commands bash curl gh jq make
  load_github_token
  cd "${REPO_ROOT}"
  check_upstream_tag

  local release_json
  release_json="$(draft_release_json)" || die "GitHub release ${TAG} does not exist"
  validate_release_identity "${release_json}"
  [[ "$(jq -r '.isDraft' <<<"${release_json}")" == "true" ]] || die "GitHub release ${TAG} must be a draft"
  info "Staging image digest: $(registry_digest "${STAGING_MANIFEST_URL}" "${TAG}")"

  local prs successful_pr state state_label url login
  prs="$(promotion_pr_json)"
  if (($(jq 'length' <<<"${prs}") > 0)); then
    successful_pr="$(jq '[.[] | select(.state == "OPEN" or .state == "MERGED")][0] // empty' <<<"${prs}")"
    if [[ -n "${successful_pr}" ]]; then
      state="$(jq -r '.state' <<<"${successful_pr}")"
      url="$(jq -r '.url' <<<"${successful_pr}")"
      state_label="$(tr '[:upper:]' '[:lower:]' <<<"${state}")"
      info "Image promotion PR already ${state_label}: ${url}"
      print_promote_checklist
      return 0
    fi
    url="$(jq -r '.[0].url' <<<"${prs}")"
    die "image promotion PR ${url} is closed without merge"
  fi

  login="$(gh api user --jq .login)"
  local -a promote_args=(make promote-images "RELEASE_TAG=${TAG}" "USER_FORK=${login}" KPROMO_USE_SSH=false)
  if ${DRY_RUN}; then
    render_command env \
      GIT_CONFIG_COUNT=1 \
      GIT_CONFIG_KEY_0=credential.https://github.com.helper \
      "GIT_CONFIG_VALUE_0=!gh auth git-credential" \
      "${promote_args[@]}"
    print_promote_checklist
    return 0
  fi

  GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0=credential.https://github.com.helper \
    GIT_CONFIG_VALUE_0='!gh auth git-credential' \
    GH_TOKEN="${GITHUB_TOKEN}" \
    "${promote_args[@]}"
  prs="$(promotion_pr_json)"
  [[ "$(jq -r '.[0].state // empty' <<<"${prs}")" == "OPEN" ]] ||
    die "image promotion command completed, but no open PR was found"
  info "Image promotion PR: $(jq -r '.[0].url' <<<"${prs}")"
  print_promote_checklist
}

verify_release_assets() {
  local release_json="${1:?release JSON is required}"
  local asset tagged_templates
  tagged_templates="$(gh api "repos/${UPSTREAM_REPO}/contents/templates?ref=${TAG}" |
    jq -r '.[] | select(.type == "file") | .name | select(startswith("cluster-template") and endswith(".yaml"))')" ||
    die "unable to list tagged cluster templates for ${TAG}"
  [[ -n "${tagged_templates}" ]] || die "no tagged cluster templates were found for ${TAG}"
  local -a expected_assets=(infrastructure-components.yaml metadata.yaml)
  while IFS= read -r asset; do
    expected_assets+=("${asset}")
  done <<<"${tagged_templates}"

  for asset in "${expected_assets[@]}"; do
    jq -e --arg asset "${asset}" '.assets | any(.name == $asset)' <<<"${release_json}" >/dev/null ||
      die "GitHub release ${TAG} is missing asset ${asset}"
  done
}

verify_release_body() {
  local release_json="${1:?release JSON is required}"
  local changelog details_url
  changelog="$(curl -fsSL "https://raw.githubusercontent.com/${UPSTREAM_REPO}/main/CHANGELOG/${TAG}.md")" ||
    die "unable to read CHANGELOG/${TAG}.md from upstream main"
  details_url="$(details_url_from_changelog <<<"${changelog}")"
  [[ -n "${details_url}" ]] || die "upstream changelog does not contain a valid Details URL"
  details_url_matches_tag "${TAG}" "${details_url}" ||
    die "upstream changelog Details URL does not end at ${TAG}: ${details_url}"
  body_contains_details_url "${details_url}" <<<"$(jq -r '.body' <<<"${release_json}")" ||
    die "GitHub release body does not contain ${details_url}"
}

target_is_newest_published_stable() {
  local published_tag published_tags
  published_tags="$(gh api --paginate "repos/${UPSTREAM_REPO}/releases?per_page=100" \
    --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name')" ||
    die "unable to list published releases"
  while IFS= read -r published_tag; do
    is_stable_tag "${published_tag}" || continue
    [[ "${published_tag}" == "${TAG}" ]] && continue
    if ! semver_gt "${TAG}" "${published_tag}"; then
      return 1
    fi
  done <<<"${published_tags}"
  return 0
}

publish_release() {
  require_commands bash curl gh jq
  load_github_token
  check_upstream_tag

  local prs promotion_url
  prs="$(promotion_pr_json)"
  (($(jq 'length' <<<"${prs}") > 0)) || die "image promotion PR not found for ${TAG}"
  promotion_url="$(jq -r '[.[] | select(.state == "MERGED")][0].url // empty' <<<"${prs}")"
  if [[ -z "${promotion_url}" ]]; then
    promotion_url="$(jq -r '.[0].url' <<<"${prs}")"
    die "image promotion PR is not merged: ${promotion_url}"
  fi
  local staging_digest production_digest
  staging_digest="$(registry_digest "${STAGING_MANIFEST_URL}" "${TAG}")"
  production_digest="$(registry_digest "${PRODUCTION_MANIFEST_URL}" "${TAG}")"
  [[ "${production_digest}" == "${staging_digest}" ]] ||
    die "production image digest ${production_digest} does not match staging digest ${staging_digest}"
  info "Production image digest: ${production_digest}"

  local release_json
  release_json="$(draft_release_json)" || die "GitHub release ${TAG} does not exist"
  validate_release_identity "${release_json}"
  verify_release_assets "${release_json}"
  verify_release_body "${release_json}"

  if [[ "$(jq -r '.isDraft' <<<"${release_json}")" == "false" ]]; then
    info "GitHub release ${TAG} is already published and valid"
    print_publish_checklist
    return 0
  fi

  local -a publish_args=(gh release edit "${TAG}" --repo "${UPSTREAM_REPO}" --draft=false)
  if target_is_newest_published_stable; then
    publish_args+=(--latest=true)
  else
    publish_args+=(--latest=false)
  fi
  if ${DRY_RUN}; then
    render_command "${publish_args[@]}"
    print_publish_checklist
    return 0
  fi

  local confirmation
  [[ -r /dev/tty ]] || die "publishing requires an interactive TTY"
  printf 'Type "publish %s" to publish: ' "${TAG}" >/dev/tty
  IFS= read -r confirmation </dev/tty || die "unable to read confirmation"
  is_publish_confirmation "${TAG}" "${confirmation}" || die "confirmation did not match publish ${TAG}"
  "${publish_args[@]}"
  info "Published GitHub release ${TAG}"
  print_publish_checklist
}

main() {
  parse_args "$@"
  case "${STAGE}" in
    prepare) prepare_release ;;
    promote) promote_release ;;
    publish) publish_release ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
