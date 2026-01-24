package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type DN42IPInfoAdapter struct {
	apiEndpoint string
}

func NewDN42IPInfoAdapter(apiEndpoint string) GeneralIPInfoAdapter {
	return &DN42IPInfoAdapter{
		apiEndpoint: apiEndpoint,
	}
}

func (ia *DN42IPInfoAdapter) GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error) {
	urlObj, err := url.Parse(ia.apiEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid api endpoint: %v", err)
	}
	urlValues := url.Values{}
	urlValues.Add("ip", ip)
	urlObj.RawQuery = urlValues.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", urlObj.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %v", err)
	}
	defer resp.Body.Close()

	respObj := new(IPInfoLiteResponse)
	if err := json.NewDecoder(resp.Body).Decode(respObj); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
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
	if respObj.CountryCode != nil && *respObj.CountryCode != "" && *respObj.CountryCode != "-" {
		basicInfo.ISO3166Alpha2 = new(string)
		*basicInfo.ISO3166Alpha2 = *respObj.CountryCode
	}

	return basicInfo, nil
}

func (ia *DN42IPInfoAdapter) GetName() string {
	return "dn42"
}
