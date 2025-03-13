package netlink

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

func New() NetlinkLib {
	return &libWrapper{}
}

type Link interface {
	netlink.Link
}

//go:generate ../../../bin/mockgen -destination mocks/mock_netlink.go -source netlink.go
type NetlinkLib interface {
	// LinkByName finds a link by name and returns a pointer to the object.
	LinkByName(name string) (Link, error)
	// LinkSetUp enables the link device.
	// Equivalent to: `ip link set $link up`
	LinkSetUp(link Link) error
	// IsLinkAdminStateUp checks if the admin state of a link is up
	IsLinkAdminStateUp(link Link) bool
	// IPv4Addresses return the IPv4 addresses of a link
	IPv4Addresses(link Link) ([]netlink.Addr, error)
	// AddrDel delete an IP address from a link
	AddrDel(link Link, ip string) error
	// AddrAdd add an IP address to a link
	AddrAdd(link Link, ip string) error
	// GetRouteSrc returns the source IP address of a route
	GetRouteSrc(dst string) (string, error)
}

type libWrapper struct{}

// LinkByName finds a link by name and returns a pointer to the object.
func (w *libWrapper) LinkByName(name string) (Link, error) {
	return netlink.LinkByName(name)
}

// LinkSetUp enables the link device.
// Equivalent to: `ip link set $link up`
func (w *libWrapper) LinkSetUp(link Link) error {
	return netlink.LinkSetUp(link)
}

// IsLinkAdminStateUp checks if the admin state of a link is up
func (w *libWrapper) IsLinkAdminStateUp(link Link) bool {
	return link.Attrs().Flags&net.FlagUp == 1
}

// IPv4Adresses return the IPv4 addresses of a link
func (w *libWrapper) IPv4Addresses(link Link) ([]netlink.Addr, error) {
	return netlink.AddrList(link, netlink.FAMILY_V4)
}

// AddrDel delete an IP address from a link
func (w *libWrapper) AddrDel(link Link, ip string) error {
	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("failed to parse IP address %s: %w", ip, err)
	}
	return netlink.AddrDel(link, addr)
}

// AddrAdd add an IP address to a link
func (w *libWrapper) AddrAdd(link Link, ip string) error {
	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("failed to parse IP address %s: %w", ip, err)
	}
	return netlink.AddrAdd(link, addr)
}

// GetRouteSrc returns the source IP address to a destination IP address
func (w *libWrapper) GetRouteSrc(dst string) (string, error) {
	ipaddr := net.ParseIP(dst)
	if ipaddr == nil {
		return "", fmt.Errorf("failed to parse IP address %s", dst)
	}
	routes, err := netlink.RouteGet(ipaddr)
	if err != nil {
		return "", fmt.Errorf("failed to get routes for IP address %s: %w", dst, err)
	}
	if len(routes) != 1 {
		return "", fmt.Errorf("multiple routes found for IP address %s", dst)
	}
	return routes[0].Src.String(), nil
}
