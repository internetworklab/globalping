package ipinfo

import (
	"context"
)

type RFC1918IPInfoAdapter struct{}

func (adapter *RFC1918IPInfoAdapter) GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error) {
	return &BasicIPInfo{
		ASN:      "",
		ISP:      "RFC1918",
		Location: "",
	}, nil
}

func (adapter *RFC1918IPInfoAdapter) GetName() string {
	return "rfc1918"
}
