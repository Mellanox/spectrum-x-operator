/*
 Copyright 2025, NVIDIA CORPORATION & AFFILIATES

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	libnetlink "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"

	"github.com/vishvananda/netlink"
	"go.uber.org/multierr"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nvipamv1 "github.com/Mellanox/nvidia-k8s-ipam/api/v1alpha1"
)

// OvsIPaddressReconciler reconciles Spectrum-X-Config from ConfigMap
type OvsIPaddressReconciler struct {
	NodeName string
	client.Client
	Exec               exec.API
	ConfigMapNamespace string
	ConfigMapName      string
	NetlinkLib         libnetlink.NetlinkLib
}

// Reconcile handles the main reconciliation loop
func (r *OvsIPaddressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)
	logr.Info("Reconciling Spectrum-X ConfigMap")

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, cm); err != nil {
		if apiErrors.IsNotFound(err) {
			logr.Info("ConfigMap not found, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	if !cm.ObjectMeta.DeletionTimestamp.IsZero() {
		logr.Info("ConfigMap is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	spectrumXConfig, err := config.ParseConfig(cm.Data[config.ConfigMapKey])
	if err != nil {
		logr.Error(err, "Failed to parse Spectrum-X ConfigMap Data")
		return ctrl.Result{}, fmt.Errorf("failed to parse Spectrum-X ConfigMap data: %w", err)
	}
	logr.Info("Parsed Spectrum-X Config", "config", spectrumXConfig)

	var hostRails []config.HostRail
	for _, host := range spectrumXConfig.Hosts {
		if host.HostID == r.NodeName {
			hostRails = host.Rails
			break
		}
	}
	if hostRails == nil {
		return ctrl.Result{}, fmt.Errorf("invalid config, Host rails not defined. (%s)", r.NodeName)
	}
	railsMap := convertHostRailSliceToMap(hostRails)
	var errs error
	for _, mapping := range spectrumXConfig.RailDeviceMapping {
		railGatewayIP, err := r.getRailGatewayIP(railsMap, mapping.RailName)
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		if err := r.processRailDeviceMapping(ctx, mapping, railGatewayIP); err != nil {
			errs = multierr.Append(errs, err)
		}
	}
	return ctrl.Result{}, errs
}

func (r *OvsIPaddressReconciler) processRailDeviceMapping(ctx context.Context, mapping config.RailDeviceMapping, gwCIDR string) error {
	logr := log.FromContext(ctx)
	rail := mapping.RailName
	railDevice := mapping.DevName

	// Get bridge interface name associated with railDevice
	bridgeInternalIfc, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl port-to-br %s", railDevice))
	if err != nil {
		return fmt.Errorf("failed to get bridge for rail %s, device %s: %w", rail, railDevice, err)
	}
	bridgeInternalIfc = strings.TrimSpace(bridgeInternalIfc)

	logr.Info("Bridge Internal interface", "rail", rail, "dev", railDevice, "interface", bridgeInternalIfc)

	// Bring up the bridge interface
	logr.Info("Set Admin Up Bridge Internal interface if needed", "interface", bridgeInternalIfc)
	if err := r.setInterfaceUp(bridgeInternalIfc); err != nil {
		return fmt.Errorf("failed to set OVS internal interface state up %s: %w", bridgeInternalIfc, err)
	}

	// Get all IPs from bridge interface
	bridgeAddrs, err := r.getIPsFromInterface(bridgeInternalIfc)
	if err != nil {
		return fmt.Errorf("failed to get IPs from OVS bridge interface %s: %w", bridgeInternalIfc, err)
	}

	// Convert bridge IPs to a set for easy lookup
	bridgeIPSet := make(map[string]bool)
	for _, addr := range bridgeAddrs {
		bridgeIPSet[addr.IP.String()] = true
	}

	// Ensure the bridge has the gateway IP (with subnet)
	gwIP := strings.Split(gwCIDR, "/")[0] // Extract only the IP
	if _, exists := bridgeIPSet[gwIP]; !exists {
		logr.Info("Adding missing gateway IP to bridge", "interface", bridgeInternalIfc, "gwCIDR", gwCIDR)
		if err := r.addIPToInterface(bridgeInternalIfc, gwCIDR); err != nil {
			return fmt.Errorf("failed to add gateway IP %s to OVS bridge interface %s: %w", gwCIDR, bridgeInternalIfc, err)
		}
	}

	// Get the IP and subnet from railDevice
	railAddrs, err := r.getIPsFromInterface(railDevice)
	if err != nil {
		return fmt.Errorf("failed to get IP from rail %s, device %s: %w", rail, railDevice, err)
	}

	if len(railAddrs) == 0 {
		logr.Info("No IP found on rail device", "rail", rail, "dev", railDevice)
		return nil
	}

	// Extract the full CIDR (IP + subnet mask)
	railCIDR := railAddrs[0].String()
	// Ensure railCIDR is correctly formatted (strip unwanted text)
	railCIDR = strings.Fields(railCIDR)[0] // Keep only the IP/CIDR part
	railIP := railAddrs[0].IP.String()
	logr.Info("Rail IP with subnet", "rail", rail, "dev", railDevice, "CIDR", railCIDR)

	// Ensure the bridge has the rail IP (with subnet) and move it from railDevice if needed
	if _, exists := bridgeIPSet[railIP]; !exists {
		logr.Info("Moving rail IP to bridge", "rail", rail, "dev", railDevice, "IP", railCIDR, "bridge", bridgeInternalIfc)

		// Add railIP (with original subnet mask) to the bridge interface
		if err := r.addIPToInterface(bridgeInternalIfc, railCIDR); err != nil {
			return fmt.Errorf("failed to add rail IP %s to OVS bridge interface %s: %w", railCIDR, bridgeInternalIfc, err)
		}
	}

	// Remove railIP (with original subnet mask) from the physical railDevice
	if err := r.removeIPFromInterface(railDevice, railCIDR); err != nil {
		return fmt.Errorf("failed to remove rail IP %s from physical port %s: %w", railCIDR, railDevice, err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *OvsIPaddressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	configMapPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		cm, ok := obj.(*corev1.ConfigMap)
		return ok && cm.Namespace == r.ConfigMapNamespace && cm.Name == r.ConfigMapName
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("OvsIPaddressReconciler").
		For(&corev1.ConfigMap{}).
		WithEventFilter(configMapPredicate).
		Complete(r)
}

func (r *OvsIPaddressReconciler) getIPsFromInterface(ifaceName string) ([]netlink.Addr, error) {
	link, err := r.NetlinkLib.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}
	addrs, err := r.NetlinkLib.IPv4Addresses(link)
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses for interface %s: %w", ifaceName, err)
	}
	return addrs, nil
}

func (r *OvsIPaddressReconciler) removeIPFromInterface(ifaceName, ipAddr string) error {
	link, err := r.NetlinkLib.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}
	return r.NetlinkLib.AddrDel(link, ipAddr)
}

func (r *OvsIPaddressReconciler) setInterfaceUp(ifaceName string) error {
	link, err := r.NetlinkLib.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}
	if !r.NetlinkLib.IsLinkAdminStateUp(link) {
		return r.NetlinkLib.LinkSetUp(link)
	}
	return nil
}

func (r *OvsIPaddressReconciler) addIPToInterface(ifaceName, ipAddr string) error {
	link, err := r.NetlinkLib.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}
	return r.NetlinkLib.AddrAdd(link, ipAddr)
}

// convertHostRailSliceToMap converts a slice of HostRail to a map with Name as the key
func convertHostRailSliceToMap(hostRails []config.HostRail) map[string]config.HostRail {
	hostRailMap := make(map[string]config.HostRail, len(hostRails))
	for _, hr := range hostRails {
		hostRailMap[hr.Name] = hr
	}
	return hostRailMap
}

func (r *OvsIPaddressReconciler) getRailGatewayIP(railsMap map[string]config.HostRail, railName string) (string, error) {
	rail, ok := railsMap[railName]
	if !ok {
		return "", fmt.Errorf("missing rail %s config for host: %s", railName, r.NodeName)
	}

	_, ipNet, err := net.ParseCIDR(rail.Network)
	if err != nil {
		return "", fmt.Errorf("failed to parse CIDR for rail %s: %w", railName, err)
	}

	// Get the gateway IP
	gwIP := nvipamv1.GetGatewayForSubnet(ipNet, 1)

	// Extract the subnet mask size correctly
	ones, _ := ipNet.Mask.Size() // Capture both values, use `ones` for the mask

	// Return the gateway IP in full CIDR format
	gwCIDR := fmt.Sprintf("%s/%d", gwIP, ones)

	return gwCIDR, nil
}
