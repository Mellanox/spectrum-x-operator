[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mellanox/spectrum-x-operator)](https://goreportcard.com/report/github.com/Mellanox/spectrum-x-operator)
[![Coverage Status](https://coveralls.io/repos/github/Mellanox/spectrum-x-operator/badge.svg)](https://coveralls.io/github/Mellanox/spectrum-x-operator)
[![Build, Test, Lint](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/build-test-lint.yml/badge.svg?event=push)](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/build-test-lint.yml)
[![CodeQL](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/codeql.yml/badge.svg)](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/codeql.yml)
[![Image push](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/image-push-main.yml/badge.svg?event=push)](https://github.com/Mellanox/spectrum-x-operator/actions/workflows/image-push-main.yml)

# NVIDIA Spectrum-X Operator
Operator that Orchestrates NVIDIA Spectrum-X networking for Kubernetes.
This Operator is intended to be deployed via [NVIDIA Network Operator](https://github.com/Mellanox/network-operator)
and should not be regarded as a standalone component.

The xplane DaemonSet inherits tolerations from the flowcontroller DaemonSet's
Pod template, including subsequent changes and removals. The flowcontroller
requires `POD_NAME` and `POD_NAMESPACE` from the downward API to identify its
owning DaemonSet; deployment manifests must provide both when upgrading the
operator. Failure to resolve that owner leaves existing xplane resources unchanged
and retries reconciliation.

Network Operator Documentation can be found [here](https://docs.nvidia.com/networking/software/cloud-orchestration/index.html)

See [DMS host plan bindings](docs/dms-plan-params.md) for structured blueprint
ConfigMaps and per-host DMSPlanParams status.

> __NOTE__: This project is currently under active development and should be considered Tech Preview only.
