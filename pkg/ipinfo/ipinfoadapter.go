package ipinfo

import (
	"context"
	"fmt"
	"math/rand"
)

type IPInfoProviderRegistry struct {
	registeredAdapters map[string]GeneralIPInfoAdapter
}

func NewIPInfoProviderRegistry() *IPInfoProviderRegistry {
	return &IPInfoProviderRegistry{
		registeredAdapters: make(map[string]GeneralIPInfoAdapter),
	}
}

func (reg *IPInfoProviderRegistry) RegisterAdapter(adapter GeneralIPInfoAdapter) {
	reg.registeredAdapters[adapter.GetName()] = adapter
}

func (reg *IPInfoProviderRegistry) GetAdapter(name string) (GeneralIPInfoAdapter, error) {
	if adapter, ok := reg.registeredAdapters[name]; ok {
		return adapter, nil
	}
	return nil, fmt.Errorf("adapter %s not found", name)
}

type ExactLocation struct {
	Longitude, Latitude float64
}

func RandomLocation() *ExactLocation {
	randomLatitude := rand.Float64()*180 - 90
	randomLongitude := rand.Float64()*360 - 180

	return &ExactLocation{
		Longitude: randomLongitude,
		Latitude:  randomLatitude,
	}
}

type BasicIPInfo struct {
	ASN      string
	Location string
	ISP      string

	// country is optional
	Country *string
	// region is optional
	Region *string
	// city is optional
	City *string
	// exact location is optional
	Exact *ExactLocation
}

type GeneralIPInfoAdapter interface {
	// return ipinfo for the querying ip address
	GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error)

	// return the name of the ipinfo adapter, although mostly there's only one adapter for each ipinfo provider
	GetName() string
}
