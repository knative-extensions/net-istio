# Istio manifests for test

This repository is testing two versions of [Istio](https://istio.io/) - `isito-head` and `istio-latest`.

- [`istio-head`](third_party/istio-head/) downloads the nightly Istio build for test.
- [`istio-latest`](third_party/istio-latest/) downloads the latest Istio release for test.

The download scripts are hosted under [`third_party`](third_party) directory.

## Bump Istio manifests

Currently the manifest files of Istio under the [`third_party`](third_party) directory must be updated manually when Istio releases the latest version.

Before bumping Istio version, you must install these tools:

- [`yq (v4)`](https://github.com/mikefarah/yq): For updating manifest files.

### Bump istio-latest manifests

The following steps show how you can bump the `istio-latest` manifest:

#### 1. Set the version number to bump

  The version number is defined in `third_party/istio-latest/generate-manifests.sh`. For example, if you want to bump the Istio version to `1.13.2`,
  you can edit `third_party/istio-latest/generate-manifests.sh` and set the version as:

  ```sh
  generate "1.13.2" "$(dirname $0)"
  ```

#### 2. Run `hack/update-codegen.sh`

  The script updates the manifest automatically so you just kick it:

  ```sh
  ./hack/update-codegen.sh
  ```

#### 3. Send the PR

  Once the manifest is updated, please send the PR against net-istio repository. The CI starts using the updated manifest and detects any issue if exists.

### Bump istio-head manifests

The following steps show how you can bump the `istio-head` manifest:

#### 1. Uncomment the script for istio-head

  The istio-head manifest script is commented out in `hack/update-codegen.sh` to avoid frequent update.
  You can edit `hack/update-codegen.sh` and uncomment the script as:

  ```sh
  ${REPO_ROOT_DIR}/third_party/istio-head/generate-manifests.sh
  ```

#### 2. Run `hack/update-codegen.sh`

  The script updates the manifest automatically so you just kick it:

  ```sh
  ./hack/update-codegen.sh
  ```

#### 3. Revert `hack/update-codegen.sh`

  Once manifest files are updated, revert the temporary change which was done by step 1.

  ```sh
  # ${REPO_ROOT_DIR}/third_party/istio-head/generate-manifests.sh
  ```

#### 4. Send the PR

  Once the manifest was updated, please send the PR against net-istio repository. The CI starts using the updated manifest and detects any issue if exists.
