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
# shellcheck source=hack/release.sh
source "${REPO_ROOT}/hack/release.sh"

assert_equal() {
  local expected="${1}"
  local actual="${2}"
  local description="${3}"
  if [[ "${expected}" != "${actual}" ]]; then
    printf 'FAIL: %s\nexpected: %q\nactual:   %q\n' "${description}" "${expected}" "${actual}" >&2
    exit 1
  fi
}

assert_true() {
  local description="${1}"
  shift
  if ! "$@"; then
    printf 'FAIL: %s\n' "${description}" >&2
    exit 1
  fi
}

assert_false() {
  local description="${1}"
  shift
  if "$@"; then
    printf 'FAIL: %s\n' "${description}" >&2
    exit 1
  fi
}

test_stable_tags() {
  assert_true "accepts a stable tag" is_stable_tag v1.26.0
  assert_true "accepts multi-digit versions" is_stable_tag v12.345.6789
  assert_false "rejects missing v prefix" is_stable_tag 1.26.0
  assert_false "rejects prereleases" is_stable_tag v1.26.0-beta.1
  assert_false "rejects build metadata" is_stable_tag v1.26.0+build
  assert_false "rejects partial versions" is_stable_tag v1.26
  assert_false "rejects a leading-zero major version" is_stable_tag v01.26.0
  assert_false "rejects a leading-zero minor version" is_stable_tag v1.026.0
  assert_false "rejects a leading-zero patch version" is_stable_tag v1.26.00
}

test_release_branches() {
  assert_equal "main" "$(release_branch_for_tag v1.26.0)" "minor releases use main"
  assert_equal "release-1.26" "$(release_branch_for_tag v1.26.1)" "patch releases use their release branch"
  assert_equal "release-12.345" "$(release_branch_for_tag v12.345.6)" "branch parsing uses integers"
}

test_semver_comparison() {
  assert_true "major version compares numerically" semver_gt v10.0.0 v9.99.99
  assert_true "minor version compares numerically" semver_gt v1.10.0 v1.9.99
  assert_true "patch version compares numerically" semver_gt v1.2.10 v1.2.9
  assert_false "equal versions are not greater" semver_gt v1.2.3 v1.2.3
  assert_false "older versions are not greater" semver_gt v1.2.2 v1.2.3
}

test_argument_parsing() {
  parse_args prepare --tag v1.26.0 --dry-run --acknowledge-image-job-status
  assert_equal "prepare" "${STAGE}" "stage is parsed"
  assert_equal "v1.26.0" "${TAG}" "tag is parsed"
  assert_equal "true" "${DRY_RUN}" "dry-run is parsed"
  assert_equal "true" "${ACKNOWLEDGE_IMAGE_JOB_STATUS}" "acknowledgement is parsed"

  parse_args promote --tag v1.26.1
  assert_equal "false" "${DRY_RUN}" "dry-run resets between parses"
  assert_equal "false" "${ACKNOWLEDGE_IMAGE_JOB_STATUS}" "acknowledgement resets between parses"

  assert_false "missing tag is rejected" bash -c \
    "source '${REPO_ROOT}/hack/release.sh'; parse_args publish --dry-run" 2>/dev/null
  assert_false "prerelease tag is rejected" bash -c \
    "source '${REPO_ROOT}/hack/release.sh'; parse_args promote --tag v1.26.0-rc.1" 2>/dev/null
  assert_false "prepare-only argument is rejected elsewhere" bash -c \
    "source '${REPO_ROOT}/hack/release.sh'; parse_args publish --tag v1.26.0 --acknowledge-image-job-status" 2>/dev/null
}

test_remote_slugs() {
  assert_equal "owner/repo" "$(remote_slug https://github.com/owner/repo.git)" "parses an HTTPS GitHub remote"
  assert_equal "owner/repo" "$(remote_slug git@github.com:owner/repo.git)" "parses an SCP-style GitHub remote"
  assert_equal "owner/repo" "$(remote_slug ssh://git@github.com/owner/repo.git)" "parses an SSH GitHub remote"
  assert_false "rejects a bare repository slug" remote_slug owner/repo
  assert_false "rejects an absolute local path" remote_slug /owner/repo
  assert_false "rejects a file URL" remote_slug file:///owner/repo
  assert_false "rejects non-GitHub HTTPS remotes" remote_slug https://example.com/owner/repo
}

test_dry_run_rendering() {
  DRY_RUN=true
  local output marker
  marker="$(mktemp)"
  rm -f "${marker}"
  output="$(run_mutation touch "${marker}")"
  assert_equal "+ touch ${marker}" "${output}" "dry-run renders a shell-safe command"
  assert_false "dry-run does not execute the command" test -e "${marker}"
}

test_registry_digest_parsing() {
  local digest headers
  digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  headers=$'HTTP/2 200\r\ncontent-type: application/json\r\nDocker-Content-Digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n\r\nHTTP/2 200\r\ndocker-content-digest: '"${digest}"$'\r\n'
  assert_equal "${digest}" "$(registry_digest_from_headers <<<"${headers}")" "last manifest digest is parsed case-insensitively"
}

test_publish_confirmation() {
  assert_true "exact confirmation is accepted" is_publish_confirmation v1.26.0 "publish v1.26.0"
  assert_false "leading whitespace is rejected" is_publish_confirmation v1.26.0 " publish v1.26.0"
  assert_false "trailing whitespace is rejected" is_publish_confirmation v1.26.0 "publish v1.26.0 "
  assert_false "wrong tag is rejected" is_publish_confirmation v1.26.0 "publish v1.26.1"
  assert_false "additional text is rejected" is_publish_confirmation v1.26.0 "yes publish v1.26.0"
}

test_details_url_validation() {
  assert_true "Details URL accepts the target tag" details_url_matches_tag \
    v1.26.1 \
    "https://github.com/kubernetes-sigs/cluster-api-provider-azure/compare/v1.26.0...v1.26.1"
  assert_false "Details URL rejects a different target tag" details_url_matches_tag \
    v1.27.0 \
    "https://github.com/kubernetes-sigs/cluster-api-provider-azure/compare/v1.26.0...v1.26.1"
}

test_release_body_line_endings() {
  local details_url
  details_url="https://github.com/kubernetes-sigs/cluster-api-provider-azure/compare/v1.26.0...v1.26.1"
  assert_true "Details URL accepts GitHub CRLF line endings" body_contains_details_url \
    "${details_url}" <<<"header"$'\r\n'"${details_url}"$'\r\n'
}

test_token_precedence() {
  GITHUB_TOKEN="selected-token"
  GH_TOKEN="different-token"
  load_github_token
  assert_equal "${GITHUB_TOKEN}" "${GH_TOKEN}" "GitHub commands use the selected token"
  unset GITHUB_TOKEN GH_TOKEN
}

test_stable_tags
test_release_branches
test_semver_comparison
test_argument_parsing
test_remote_slugs
test_dry_run_rendering
test_registry_digest_parsing
test_publish_confirmation
test_details_url_validation
test_release_body_line_endings
test_token_precedence

printf 'release tests passed\n'
