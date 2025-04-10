# Spectrum X Operator Deployment

> __NOTE__: This project is currently under active development and should be considered Tech Preview only.

## Prerequisites

### Host configuration

Please refer to the [Setup Guide](setup-reference.md) for the hosts configuration.

### Helm

Install `helm`:

```bash
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 \
  && chmod 700 get_helm.sh \
  && ./get_helm.sh
```

## Install Network Operator

Add NVIDIA NGC Helm repository

```bash
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
```

Update Helm repositories

```bash
helm repo update
```

Install Network Operator with SR-IOV Network Operator (with `manageSoftwareBridges` feature gate):

```bash
helm install network-operator nvidia/network-operator \
   -n nvidia-network-operator \
   --create-namespace \
   --version v25.4.0 \
  --set sriovNetworkOperator.enabled=true \
  --set sriov-network-operator.sriovOperatorConfig.featureGates.manageSoftwareBridges=true
```

### Disable "mellanox" plugin in SR-IOV Network Operator

Since firmware configuration is already done, disable `mellanox` plugin to avoid un-needed drain/reboot.

```bash
kubectl patch sriovoperatorconfigs.sriovnetwork.openshift.io -n nvidia-network-operator default --patch '{ "spec": { "disablePlugins": ["mellanox"] } }' --type='merge'
```

### Create NicClusterPolicy

```bash
cat <<EOF | kubectl create -f -
apiVersion: mellanox.com/v1alpha1
kind: NicClusterPolicy
metadata:
  name: nic-cluster-policy
spec:
  spectrumXOperator:
    image: spectrum-x-operator
    repository: ghcr.io/mellanox
    version: latest
    imagePullSecrets: []
  nvIpam:
    image: nvidia-k8s-ipam
    repository: ghcr.io/mellanox
    version: v0.3.7
    imagePullSecrets: []
    enableWebhook: false
  secondaryNetwork:
    cniPlugins:
      image: plugins
      repository: ghcr.io/k8snetworkplumbingwg
      version: v1.2.0-amd64
    multus:
      image: multus-cni
      imagePullSecrets: []
      repository: ghcr.io/k8snetworkplumbingwg
      version: v4.1.0
EOF
```

## Deploy SpectrumX Operator configmap

See example of configmap [here](./examples/spectrum-x-config.yaml).