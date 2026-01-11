package ipinfo

import (
	"context"
	"fmt"

	pkgrouting "example.com/rbmq-demo/pkg/routing"
)

type AutoIPInfoDispatcher struct {
	Router pkgrouting.Router
}

func (autoProvider *AutoIPInfoDispatcher) SetUpDefaultRoutes(
	dn42Provider GeneralIPInfoAdapter,
	internetIPInfoProvider GeneralIPInfoAdapter,
) {
	dn42Net := "172.20.0.0/14"
	dn42Net6 := "fd00::/8"
	neoNet := "10.127.0.0/16"
	ianaNet := "0.0.0.0/0"
	ianaNet6 := "::/0"

	autoProvider.AddRoute(dn42Net, dn42Provider)
	autoProvider.AddRoute(dn42Net6, dn42Provider)
	autoProvider.AddRoute(neoNet, dn42Provider)
	autoProvider.AddRoute(ianaNet, internetIPInfoProvider)
	autoProvider.AddRoute(ianaNet6, internetIPInfoProvider)

	rfc1918IPInfoAdapter := &RFC1918IPInfoAdapter{}
	autoProvider.AddRoute("10.0.0.0/8", rfc1918IPInfoAdapter)
	autoProvider.AddRoute("172.16.0.0/12", rfc1918IPInfoAdapter)
	autoProvider.AddRoute("192.168.0.0/16", rfc1918IPInfoAdapter)
}

func (autoProvider *AutoIPInfoDispatcher) AddRoute(prefix string, provider GeneralIPInfoAdapter) {
	autoProvider.Router.AddRoute(prefix, provider)
}

func (autoProvider *AutoIPInfoDispatcher) GetIPInfo(ctx context.Context, ipAddr string) (*BasicIPInfo, error) {
	ipinfoProviderRaw, err := autoProvider.Router.GetRoute(ipAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get ipinfo for %s: %v", ipAddr, err)
	}

	ipinfoProvider, ok := ipinfoProviderRaw.(GeneralIPInfoAdapter)
	if !ok {
		panic(fmt.Sprintf("ipinfo provider for %s is not a GeneralIPInfoAdapter", ipAddr))
	}

	return ipinfoProvider.GetIPInfo(ctx, ipAddr)
}

func (autoProvider *AutoIPInfoDispatcher) GetName() string {
	return "auto"
}
