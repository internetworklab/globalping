package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

type IPInfoLiteResponse struct {
	ASN     *string `json:"asn,omitempty"`
	ASName  *string `json:"as_name,omitempty"`
	Country *string `json:"country,omitempty"`
}

type IPInfoAdapter struct {
	Token *string
}

func NewIPInfoAdapter(token *string) (GeneralIPInfoAdapter, error) {
	if token != nil && *token != "" {
		log.Printf("Using IPINFO_TOKEN: %s", *token)
	} else {
		return nil, fmt.Errorf("IPInfo adapter relies on a token to work, however none is provided")
	}
	return &IPInfoAdapter{
		Token: token,
	}, nil
}

func (ia *IPInfoAdapter) GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error) {
	urlObj, err := url.Parse(fmt.Sprintf(`https://api.ipinfo.io/lite/%s`, ip))
	if err != nil {
		return nil, fmt.Errorf("invalid ip: %s", ip)
	}

	if ia.Token != nil && *ia.Token != "" {
		urlValues := url.Values{}
		urlValues.Add("token", *ia.Token)
		urlObj.RawQuery = urlValues.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlObj.String(), nil)
	if err != nil {
		// err might have sensitive info, so we don't return it as-is, the admin can just review the logs however.
		log.Printf("failed to create request for ipinfo query %s: %v", ip, err)
		return nil, fmt.Errorf("failed to create request for ipinfo query %s", ip)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// err might have sensitive info, so we don't return it as-is, the admin can just review the logs however.
		log.Printf("failed to invoke ipinfo query for %s: %v", ip, err)
		return nil, fmt.Errorf("failed to invoke ipinfo query for %s", ip)
	}

	respObj := new(IPInfoLiteResponse)
	if err := json.NewDecoder(resp.Body).Decode(respObj); err != nil {
		return nil, fmt.Errorf("failed to decode ipinfo response for %s: %v", ip, err)
	}

	basicInfo := new(BasicIPInfo)
	if respObj.ASN != nil && *respObj.ASN != "" {
		basicInfo.ASN = *respObj.ASN
	}
	if respObj.ASName != nil && *respObj.ASName != "" {
		basicInfo.ISP = *respObj.ASName
	}
	if respObj.Country != nil && *respObj.Country != "" {
		basicInfo.Location = *respObj.Country
	}

	return basicInfo, nil
}

func (ia *IPInfoAdapter) GetName() string {
	return "ipinfo"
}
