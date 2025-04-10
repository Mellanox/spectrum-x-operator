# Spectrum X environment setup

> __NOTE__: This project is currently under active development and should be considered Tech Preview only.

This guide is a reference for setting up a testing environment.


## DGX setup

### SR-IOV BIOS Configuration

Verify that SR-IOV is enabled in the BIOS.

The following command checks the SR-IOV capability for the ConnectX-7 cards:

```bash
for pci in $(lspci | grep "ConnectX-7" | awk '{print $1}'); do
  echo "Checking device at PCI ID: $pci"
  lspci -s "$pci" -v | grep -i "SR-IOV"
  echo "-----------------------------"
done
```

Example output:

```bash
Checking device at PCI ID: 0c:00.0
	Capabilities: [180] Single Root I/O Virtualization (SR-IOV)
-----------------------------
Checking device at PCI ID: 12:00.0
	Capabilities: [180] Single Root I/O Virtualization (SR-IOV)
-----------------------------
...
```

If the capability is not present, access BIOS setup menu during boot and enable SR-IOV.

### Huge Pages

#### Configure system to create hugepages on boot

```bash
echo "vm.nr_hugepages=32768" > /etc/sysctl.d/99-hugepages.conf
```

To apply immediately (and match the boot-time config):

```bash
sysctl -w vm.nr_hugepages=32768
```

### Mount Huge Pages FS

```bash
mkdir -p /hugepages
mount -t hugetlbfs hugetlbfs /hugepages
```


To make it persistent, add the following line to `/etc/fstab` file:

```bash
none /hugepages hugetlbfs defaults 0 0
```


### Set Huge Pages on all the NUMA Nodes

The following steps set 4096 hugepages (2MB each) per NUMA node, totaling 64GB (8 nodes × 4096 × 2MB).

Create file: `/etc/systemd/system/hugepages-numa.service` with following content:

```bash
[Unit]
Description=Configure HugePages on all NUMA nodes
After=multi-user.target

[Service]
Type=oneshot
ExecStart=/usr/bin/bash -c 'for node in /sys/devices/system/node/node*/hugepages/hugepages-2048kB; do echo 4096 > "$node/nr_hugepages"; done'

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
systemctl daemon-reload
systemctl enable hugepages-numa.service
systemctl start hugepages-numa.service
```

Confirm the settings:

```bash
for node in /sys/devices/system/node/node*/hugepages/hugepages-2048kB; do
  echo -n "$node: "
  cat "$node/nr_hugepages"
done
```

#### Verify Huge Pages

```bash
# sysctl vm.nr_hugepages
vm.nr_hugepages = 32768
```

```bash
# grep Huge /proc/meminfo
AnonHugePages:         0 kB
ShmemHugePages:        0 kB
FileHugePages:         0 kB
HugePages_Total:   32768
HugePages_Free:    32768
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:        67108864 kB

```

### Install DOCA-Host

```bash
export DOCA_URL="https://linux.mellanox.com/public/repo/doca/2.10.0/ubuntu22.04/x86_64/"
curl https://linux.mellanox.com/public/repo/doca/GPG-KEY-Mellanox.pub | gpg --dearmor > /etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub
echo "deb [signed-by=/etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub] $DOCA_URL ./" > /etc/apt/sources.list.d/doca.list
apt-get update
apt-get -y install doca-all
```

### Setup Firmware Settings

The following firmware settings are required: `SRIOV_EN=True` and `NUM_OF_VFS=8`.

The following script check the required configuration and update if needed.

Note that if a change has been done, node reboot is required for changes to take effect.

```bash
#!/bin/bash

# Loop through all ConnectX-7 PCI devices
for pci in $(lspci | grep "ConnectX-7" | awk '{print $1}'); do
    echo "Checking device at PCI ID: $pci"

    # Query current settings
    output=$(mlxconfig -y -d $pci q | grep -E 'SRIOV_EN|NUM_OF_VFS')
    echo "$output"

    sriov_en=$(echo "$output" | grep SRIOV_EN | awk '{print $2}')
    num_vfs=$(echo "$output" | grep NUM_OF_VFS | awk '{print $2}')

    # Check and configure if needed
    if [[ "$sriov_en" != "True(1)" || "$num_vfs" != "8" ]]; then
        echo "Configuring SRIOV_EN=1 and NUM_OF_VFS=8 on $pci"
        mlxconfig -y -d $pci set SRIOV_EN=1 NUM_OF_VFS=8
        echo "✅ Configuration updated. Reboot required for changes to take effect."
    else
        echo "✅ SR-IOV is already correctly configured on $pci"
    fi

    echo "-----------------------------"
done
```

### OVS Setup

Set `other_config` parameters:

```bash
ovs-vsctl set Open_vSwitch . other_config:doca-init=true
ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
ovs-vsctl set Open_vSwitch . other_config:doca-eswitch-max=8
ovs-vsctl set Open_vSwitch . other_config:hw-offload-ct-size=0
```

Restart OVS:

```bash
systemctl restart openvswitch-switch
```

### Set MTU on PFs and create VFs in switchdev mode

If netplan is used to configure the NICs, it is possible to set MTU and VF in the configuration. For example:

```yaml
  ethernets:
    enp12s0np0:
      mtu: 9216
      embedded-switch-mode: switchdev
      virtual-function-count: 1
```

Otherwise, use following script to set MTU, switchdev and create VF:

```bash
#!/bin/bash
set -euo pipefail

# Default MTU (can be overridden with an argument)
MTU=${1:-9000}

echo "Starting ConnectX-7 NIC configuration with MTU=$MTU"

# Loop over all ConnectX-7 PFs
for pci_dev in $(lspci -D | grep -i "ConnectX-7" | grep -v "Virtual Function" | awk '{print $1}'); do
    echo "Configuring PCI device: $pci_dev"

    netdev_path="/sys/bus/pci/devices/$pci_dev/net"
    if [ ! -d "$netdev_path" ]; then
        echo "  !! No netdev found for $pci_dev, skipping"
        continue
    fi

    netdev=$(basename "$(readlink -f "$netdev_path"/*)")
    echo "  -> Associated netdev: $netdev"

    # Set MTU on PF first
    echo "  -> Setting MTU=$MTU on $netdev"
    ip link set "$netdev" mtu "$MTU" || echo "  !! Failed to set MTU on $netdev"

    # Set switchdev mode
    echo "  -> Setting eswitch mode to switchdev"
    devlink dev eswitch set pci/$pci_dev mode switchdev

    # Enable 1 VF
    sriov_path="/sys/class/net/$netdev/device/sriov_numvfs"
    if [ -w "$sriov_path" ]; then
        echo "  -> Enabling 1 VF on $netdev"
        echo 1 > "$sriov_path"
    else
        echo "  !! $sriov_path not writable or missing. Skipping VF creation"
    fi

    echo
done

echo "✅ Done configuring ConnectX-7 NICs."
```

### NIC IPs

The NICs needs to have IPs configured according to the SpectrumX IP plan before running the SpectrumX Operator.

## Install Kubernetes

### Disable Swap:

```bash
swapoff -a
sed -i '/ swap / s/^/#/' /etc/fstab
systemctl mask swap.target
```

### Load Kernel Modules for Kubernetes Networking

```bash
cat <<EOF | tee /etc/modules-load.d/containerd.conf
overlay
br_netfilter
EOF

modprobe overlay
modprobe br_netfilter
```

### Set Required Sysctl Params

```bash
cat <<EOF | tee /etc/sysctl.d/kubernetes.conf
net.bridge.bridge-nf-call-ip6tables = 1
net.bridge.bridge-nf-call-iptables = 1
net.ipv4.ip_forward = 1
EOF

sysctl --system
```

### Install Container Runtime

```bash
apt install -y containerd
```

### Generate default containerd config:

```bash
mkdir -p /etc/containerd
containerd config default | tee /etc/containerd/config.toml
```

### Update to use systemd cgroups (recommended):

```bash
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
```

### Restart containerd:

```bash
systemctl restart containerd
systemctl enable containerd
```

### Add Kubernetes APT Repository

```bash
# Install required dependencies
apt-get install -y apt-transport-https ca-certificates curl gpg

# Download and install the Kubernetes GPG key
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.30/deb/Release.key | \
  gpg --dearmor -o /usr/share/keyrings/kubernetes-archive-keyring.gpg

# Add the Kubernetes apt repository
echo "deb [signed-by=/usr/share/keyrings/kubernetes-archive-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.30/deb/ /" | \
  tee /etc/apt/sources.list.d/kubernetes.list > /dev/null

# Update apt package index
apt-get update
```

### Install Kubernetes Components

```bash
apt-get install -y kubelet kubeadm kubectl
apt-mark hold kubelet kubeadm kubectl
```

### Create control-plane node

```bash
kubeadm init  --pod-network-cidr=10.244.0.0/16
```

Keep the token for other node.

Configure `kubectl` access:

```bash
mkdir -p $HOME/.kube
cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
chown $(id -u):$(id -g) $HOME/.kube/config
```

Remove taint from control-plane node:

```bash
kubectl taint nodes $(kubectl get node -o jsonpath="{.items[0].metadata.name}") node-role.kubernetes.io/control-plane-
``` 

Label control-plane as worker:

```bash
kubectl label nodes $(kubectl get node -o jsonpath="{.items[0].metadata.name}") node-role.kubernetes.io/worker=
```

### Install CNI (flannel):

```bash
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
```

### Create worker node

```bash
kubeadm join <master-ip>:6443 --token <token> --discovery-token-ca-cert-hash sha256:<hash>
```

Label second node as worker:

```bash
kubectl label nodes $(kubectl get node -o jsonpath="{.items[1].metadata.name}") node-role.kubernetes.io/worker=
```