# Copyright 2025 NVIDIA CORPORATION & AFFILIATES
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
#
# SPDX-License-Identifier: Apache-2.0

# Build the manager binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG GCFLAGS
ARG GOPROXY
ENV GOPROXY=$GOPROXY

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod/ go mod download

# Copy the go source
COPY ./ ./

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN --mount=type=cache,target=/go/pkg/mod/ GO_GCFLAGS=${GCFLAGS} make build

FROM nvcr.io/nvidia/doca/doca:3.1.0-full-rt-host

RUN apt update && apt install -y \
    iproute2 \
    iputils-arping

WORKDIR /
COPY --from=builder /workspace/build/flowcontroller .
COPY --from=builder /workspace/build/railcni .
# Copy sources to /src
COPY --from=builder /workspace /src

USER 65532:65532

ENTRYPOINT ["/flowcontroller"]

LABEL org.opencontainers.image.source=https://github.com/Mellanox/spectrum-x-operator
