# Istio manifests for test

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
