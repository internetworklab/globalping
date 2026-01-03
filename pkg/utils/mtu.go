package utils

import (
	"fmt"
	"net"
	"sort"

	"github.com/vishvananda/netlink"
)

func GetMaximumMTU() int {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	maximumMTU := -1
	for _, iface := range ifaces {
		if iface.MTU > maximumMTU {
			maximumMTU = iface.MTU
		}
	}
	if maximumMTU == -1 {
		panic("can't determine maximum MTU")
	}
	return maximumMTU
}

const standardMTU int = 1500

func GetMinimumMTU() int {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}

	mtuVals := make([]int, 0)
	for _, iface := range ifaces {
		mtuVals = append(mtuVals, iface.MTU)
	}

	if len(mtuVals) == 0 {
		return standardMTU
	}

	minMTU := mtuVals[0]
	for _, mtu := range mtuVals {
		if mtu < minMTU {
			minMTU = mtu
		}
	}

	return minMTU
}

func GetNexthopMTU(destination net.IP, considerPMTUCache bool) int {
	mtus := make([]int, 0)
	handle, err := netlink.NewHandle()
	if err != nil {
		panic(fmt.Errorf("failed to create netlink handle: %v", err))
	}
	defer handle.Close()

	routes, err := handle.RouteGet(destination)
	if err != nil {
		panic(fmt.Errorf("failed to get route for %s: %v", destination, err))
	}
	for _, route := range routes {
		link, err := netlink.LinkByIndex(route.LinkIndex)
		if err != nil {
			panic(fmt.Errorf("failed to get link by index %d: %v", route.LinkIndex, err))
		}
		if linkMtu := link.Attrs().MTU; linkMtu > 0 {
			mtus = append(mtus, linkMtu)
		}

		if considerPMTUCache {
			if routeMtu := route.MTU; routeMtu > 0 {
				mtus = append(mtus, routeMtu)
			}
		}

	}
	if len(mtus) == 0 {
		return GetMinimumMTU()
	}
	sort.Ints(mtus)
	return mtus[0]
}
