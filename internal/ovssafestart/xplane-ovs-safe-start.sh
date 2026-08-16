#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES
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

# Strip spectrum-x doca uplinks from OVSDB before ovs-vswitchd starts when the
# PF is not yet in switchdev (or the netdev does not exist yet). This avoids
# DOCA port bring-up racing with sriov-network-config-daemon switchdev apply.
set +e
LOG() { logger -t xplane-ovs-pre "$*" 2>/dev/null; echo "$(date -Is) $*" >>/var/log/xplane-ovs-pre.log 2>/dev/null; }

LOG "start"
IFACES=$(ovs-vsctl --no-wait --bare --columns=name \
  find Interface external_ids:xplane-uplink=true type=doca 2>>/var/log/xplane-ovs-pre.log)
LOG "find ifaces=[${IFACES//$'\n'/ }]"

for iface in $IFACES; do
  [ -z "$iface" ] && continue
  mode=""
  if [ -e "/sys/class/net/$iface/device" ]; then
    pci=$(basename "$(readlink -f "/sys/class/net/$iface/device")")
    mode=$(devlink dev eswitch show "pci/$pci" 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="mode"){print $(i+1); exit}}')
  fi
  if [ "$mode" != "switchdev" ]; then
    ovs-vsctl --no-wait --if-exists del-port "$iface"
    LOG "removed $iface (eswitch=${mode:-no-netdev})"
  else
    LOG "keep $iface (eswitch=switchdev)"
  fi
done
LOG "done"
exit 0
