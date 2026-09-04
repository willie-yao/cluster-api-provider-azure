# CAPZ Releases

## Release Cadence

CAPZ minor versions (that is, 1.**5**.0 versus 1.**4**.x) are typically released every two months. In order to be practical and flexible, we will consider a more rapid minor release (for example, earlier than two months following the latest minor release) if any of the following conditions are true:

- Significant set of well-tested features introduced since the last release.
  - The guiding principle here is to avoid minor releases that introduce excess feature changes, to aid release testing and validation, and to allow users the option of adopting fewer changes when upgrading.
- User demand for rapid adoption of a specific feature or feature.

Additionally, we will consider delaying a minor release if no significant features have landed during the normal two-month release cycle.

CAPZ patch versions (for example, 1.5.**2** versus 1.5.**1**) are released as often as weekly. Each week at the open office hours meeting, maintainers decide whether or not a patch release is called for based on community input. A patch release may bypass this cadence if circumstances warrant.

## Release Support

The two most recent minor releases of CAPZ will be supported with bug fixes. Assuming minor releases arrive every two months on average, each minor release will receive fixes for four months.

For example, let's assume CAPZ v1.4.2 is the current release, and v1.3.2 is the latest in the previous minor release line. When v1.5.0 is released, it becomes the current release. v1.4.2 becomes the previous release line and remains supported. And v1.3.2 reaches end-of-life and no longer receives support through bug fixes.

Note that "support" in this context refers strictly to whether or not bug fixes are backported to a release line. Please see [the support documentation](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/SUPPORT.md) for more general information about how to get help with CAPZ.

### Bug Fixes and Test Improvements

Any significant user-facing bug fix that lands in the `main` branch should be backported to the current and previous release lines. Security-related fixes are automatically considered significant and user-facing.

Improvements or significant changes to tests should be backported to the current release line. This is intended to minimize friction in the event of a critical test fix. Test improvements or changes may sometimes need to be backported to the previous release line in the event that tests break on all release branches.

### Experimental API Changes

Experimental Cluster API features (for example, `AzureManagedCluster`) may evolve more rapidly than graduated v1 features. CAPZ allows general changes, enhancements, or additions in this area to be cherry-picked into the current release branch for inclusion in patch releases. This will accelerate the effort to graduate experimental features to the stable API by allowing faster adoption and iteration.

Breaking changes are also allowed in experimental APIs; those changes will not be included in a patch release, but will be introduced in a new minor release, with appropriate release notes.

### Timing of Merges

Sometimes pull requests touch a large number of files and are more likely to create challenges for the automated cherry-pick process. In such cases, maintainers may prefer to delay merging such changes until the end of a minor release cycle.

## Release Process

The release process can be assisted by any contributor, but requires some specific steps to be be done by a maintainer as shown by (maintainer) to the right of the step title. The process is as follows:

### 1. Verify the image promotion machinery is healthy

Before starting a release, confirm that the job which builds and pushes staging images is working. This machinery only runs when promoting images, so a broken `cloudbuild.yaml` often goes unnoticed until a release is underway, forcing a follow-on patch release just to fix it.

- Check that the [post push images job](https://testgrid.k8s.io/sig-cluster-lifecycle-cluster-api-provider-azure#post-cluster-api-provider-azure-push-images) is green.
- Confirm that the image pinned in [cloudbuild.yaml](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/cloudbuild.yaml) is in sync with the one [Cluster API uses](https://github.com/kubernetes-sigs/cluster-api/blob/main/cloudbuild.yaml). If it is out of date, open a [PR](https://github.com/kubernetes-sigs/cluster-api-provider-azure/pull/6406) to update it before proceeding.

`make release-prepare` validates both checks. If the Testgrid summary is not passing for a known and accepted reason, pass `RELEASE_ARGS=--acknowledge-image-job-status`. The command never changes `cloudbuild.yaml`.

### 2. Update main metadata.yaml (skip for patch releases)

- Make sure the [metadata.yaml](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/metadata.yaml) file in the root of the project is up to date and contains the new release with the correct cluster-api contract version.
  - If not, open a [PR](https://github.com/kubernetes-sigs/cluster-api-provider-azure/pull/1928) to add it.

This must be done prior to generating release artifacts, so the release contains the correct metadata information for `clusterctl` to use.

### 3. Change milestone (skip for patch releases) (maintainer)

- Create a [new GitHub milestone](https://github.com/kubernetes-sigs/cluster-api-provider-azure/milestones/new) for the next release.
- Change the milestone applier so new changes can be applied to the appropriate release. [A sample PR](https://github.com/kubernetes/test-infra/pull/34225) in test infra to update the release.

#### Versioning

cluster-api-provider-azure follows the [semantic versionining][semver] specification.

Example versions:

- Pre-release: `v0.1.1-alpha.1`
- Minor release: `v0.1.0`
- Patch release: `v0.1.1`
- Major release: `v1.0.0`


### 4. Open a PR for release notes

1. Authenticate `gh`, or export `GITHUB_TOKEN`. The token needs permission to create pull requests in your fork. The release commands use `GITHUB_TOKEN` when it is set and otherwise use `gh auth token`.

1. Fetch the latest changes from upstream, check out `main`, and ensure it is clean and exactly matches `upstream/main`.

1. Preview the checks and actions, then prepare the release-notes pull request:

    ```sh
    RELEASE_TAG=v1.2.3 RELEASE_ARGS=--dry-run make release-prepare
    RELEASE_TAG=v1.2.3 make release-prepare
    ```

    The command validates release prerequisites, generates `CHANGELOG/<RELEASE_TAG>.md`, creates `release-notes-<RELEASE_TAG>` in the authenticated user's fork, and opens a normal pull request against `main`. It exits successfully without creating duplicates when that pull request already exists.

1. Review the generated release notes and make any necessary changes:

    - Move items out of "Uncategorized" into an appropriate section.
    - Fix any typos or other errors.

Merging the PR will automatically trigger a [Github Action](https://github.com/kubernetes-sigs/cluster-api-provider-azure/actions) to create a release branch (if needed), push a tag, and publish a draft release.

### 5. Promote image to prod repo

- Images are built by the [post push images job](https://testgrid.k8s.io/sig-cluster-lifecycle-cluster-api-provider-azure#post-cluster-api-provider-azure-push-images). This will push the image to a [staging repository][staging-repository].
- Wait for the above job to complete for the tag commit and for the image to exist in the staging directory, then preview and create the image promotion pull request:

  ```sh
  RELEASE_TAG=v1.2.3 RELEASE_ARGS=--dry-run make release-promote
  RELEASE_TAG=v1.2.3 make release-promote
  ```

This validates the upstream tag, draft release, and staging image digest before using `kpromo` to create a PR in [k8s.io](https://github.com/kubernetes/k8s.io). An existing open or merged promotion PR is treated as success. A closed, unmerged promotion PR must be resolved manually.

### 6. Review and approve promoted prod image (maintainer)

For reviewers of the above-created PR, to confirm that the resultant image SHA-to-tag addition is valid, you can check against the [staging repository][staging-repository].

Using [the above example PR](https://github.com/kubernetes/k8s.io/pull/4284), to verify that the image identified by SHA `d0636fad7f4ced58b5385615a53b7cb2053f79c4788bd299e0ac9e46a25b5053` has the expected `v1.4.3`, tag, you would inspect the image metadata by viewing it in the Google Artifact Registry UI:

- https://console.cloud.google.com/artifacts/docker/k8s-staging-cluster-api-azure/us/gcr.io/cluster-api-azure-controller

### 7. Release in GitHub (maintainer)

- Proofread the GitHub release content and fix any remaining errors. This is copied from the release notes generated earlier. Keep it as a draft until all checks pass.
- Preview the validations, then publish:

  ```sh
  RELEASE_TAG=v1.2.3 RELEASE_ARGS=--dry-run make release-publish
  RELEASE_TAG=v1.2.3 make release-publish
  ```

  The command requires the promotion PR to be merged and validates the production image digest, release assets, and Details URL. Publishing requires typing the exact confirmation `publish v1.2.3` in a TTY. The release is marked latest only when its integer semantic version is newer than every published stable release. An already published release is validated and reported as success without being republished.

### 8. Update docs (skip for patch releases) (maintainer)

Go to [the Netlify branches and deploy contexts in site settings](https://app.netlify.com/sites/kubernetes-sigs-cluster-api-provider-azure/settings/deploys#branches-and-deploy-contexts) and click "edit settings". Update the "Production branch" to the new release branch and click "Save". The, go to the [Netlify site deploys](https://app.netlify.com/sites/kubernetes-sigs-cluster-api-provider-azure/deploys) and trigger a new deploy.

![Netlify settings screenshot](images/netlify_deploys.png)

Note: this step requires access to the Netlify site. If you don't have access, please ask a maintainer to update the branch.

### 9. Update security scanner and Dependabot branches (skip for patch releases)

Open a pull request to update the branches in the [weekly security scan workflow](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/.github/workflows/weekly-security-scan.yaml) to include the new release branch. For example, if the new release branch is `release-1.23`, update the `branch` matrix to:

```yaml
      matrix:
        branch: [ main, release-1.23, release-1.22 ]
```

In the same pull request, update [dependabot.yml](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/.github/dependabot.yml) to match. Dependabot only opens pull requests against the default branch unless an entry sets `target-branch`, so each supported release branch needs its own `gomod` entry. Add one for the new release branch and delete the entry for any branch that's no longer supported:

```yaml
# Go - root directory, release-1.23 branch
- directory: "/"
  package-ecosystem: "gomod"
  target-branch: "release-1.23"
  # ...copy the rest from an existing release branch entry
```

Keep the branches listed here in sync with the security scan matrix. If they drift, the weekly scan will keep reporting vulnerabilities on a branch that Dependabot never updates.

### 10. Announce the new release

#### Patch Releases

1. Announce the release in Kubernetes Slack on the [#cluster-api-azure](https://kubernetes.slack.com/archives/CEX9HENG7) channel.

#### Minor/Major Releases

1. Follow the communications process for [patch-releases](#patch-releases)
2. An announcement email is sent to `kubernetes-sig-azure@googlegroups.com` and `kubernetes-sig-cluster-lifecycle@googlegroups.com` with the subject `[ANNOUNCE] cluster-api-provider-azure <version> has been released`

[semver]: https://semver.org/#semantic-versioning-200
[template]: /docs/release-notes-template.md
[versioning]: #versioning
[staging-repository]: https://console.cloud.google.com/artifacts/docker/k8s-staging-cluster-api-azure/us/gcr.io/cluster-api-azure-controller

## Post release steps (maintainer)

  - Open a PR in https://github.com/kubernetes/test-infra to change [this line](https://github.com/kubernetes/test-infra/blob/25db54eb9d52e08c16b3601726d8f154f8741025/config/prow/plugins.yaml#L344).
    - See an [example PR](https://github.com/kubernetes/test-infra/pull/16827).

### Update test provider versions (skip for patch releases)

This can be done in parallel with release publishing and does not impact the release or its artifacts.

#### Update test metadata.yaml

Using that same next release version used to create a new milestone, update the the CAPZ provider [metadata.yaml](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/test/e2e/data/shared/v1beta1_provider/metadata.yaml) that we use to run PR and periodic cluster E2E tests against the main branch templates. (This `metadata.yaml` is in the `test/e2e/data/shared/v1beta1_provider` directory; it's not the one in the project's root that we edited earlier.)

For example, if the latest stable API version of CAPZ that we run E2E tests against is `v1beta`, and we're releasing `v1.12.0`, and our next release version is `v1.13.0`, then we want to ensure that the `metadata.yaml` defines a contract between `v1.13.0` and `v1beta1`:

```yaml
apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3
releaseSeries:
  - major: 1
    minor: 11
    contract: v1beta1
  - major: 1
    minor: 12
    contract: v1beta1
  - major: 1
    minor: 13
    contract: v1beta1
```

Additionally, we need to update the `type: InfrastructureProvider` spec in [azure-dev.yaml](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/main/test/e2e/config/azure-dev.yaml) to express that our intent is to test (using the above example) `1.13`. By convention we use a sentinel patch version "99" to express "any patch version". In this example we want to look for the `type: InfrastructureProvider` with a `name` value of `v1.12.99` and update it to `v1.13.99`:

```yaml
    - name: v1.13.99 # "vNext"; use manifests from local source files
```

#### Update clusterctl API version upgrade tests

Update the [API version upgrade tests](https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.12.2/test/e2e/capi_test.go#L214) to use the oldest supported release versions of CAPI and CAPZ after the release is cut as "Init" provider versions. See [this PR](https://github.com/kubernetes-sigs/cluster-api-provider-azure/pull/4433) for more details.

### Update Upstream Tests (skip for patch releases)

For major and minor releases we will need to update the set of capz-dependent `test-infra` jobs so that they use our latest release branch. For example, if we cut a new `1.3.0` minor release, from a newly created `release-1.3` git branch, then we need to update all test jobs to use capz at `release-1.3` instead of `release-1.2`.

Here is a reference PR that applied the required test job changes following the `1.3.0` minor release described above:

- [Reference test-infra PR](https://github.com/kubernetes/test-infra/pull/26200)

#### Roadmap (maintainer)

Consider whether anything should be updated in the [roadmap document](../roadmap.md) by answering the following questions:

1. Have any of the Epics listed been entirely or largely achieved?  If so, then the Epic should likely be removed and highlighted during the release communications.
2. Are there any new Epics we want to highlight?  If so, then consider opening a PR to add them and bringing them up in the next office hours planning meeting with the milestone review.
3. Have any updates to the roadmap document occurred in the past 6 months?  If not, it should be updated in some form.

If any changes need to be made, it should not block the release itself.
