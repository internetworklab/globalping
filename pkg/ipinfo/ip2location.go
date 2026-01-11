package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type IP2LocationIPInfoAdapter struct {
	APIEndpoint string
	APIKey      string
}

func NewIP2LocationIPInfoAdapter(apiEndpoint string, apiKey string) GeneralIPInfoAdapter {
	return &IP2LocationIPInfoAdapter{
		APIEndpoint: apiEndpoint,
		APIKey:      apiKey,
	}
}

type IP2LocationIPInfoResponse struct {
	IP          *string  `json:"ip,omitempty"`
	CountryCode *string  `json:"country_code,omitempty"`
	CountryName *string  `json:"country_name,omitempty"`
	RegionName  *string  `json:"region_name,omitempty"`
	CityName    *string  `json:"city_name,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
	ZipCode     *string  `json:"zip_code,omitempty"`
	TimeZone    *string  `json:"time_zone,omitempty"`
	ASN         *string  `json:"asn,omitempty"`
	AS          *string  `json:"as,omitempty"`
	IsProxy     *bool    `json:"is_proxy,omitempty"`
}

func (ia *IP2LocationIPInfoAdapter) GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error) {

	urlObj, err := url.Parse(ia.APIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid api endpoint: %v", err)
	}
	urlValues := url.Values{}
	urlValues.Add("ip", ip)
	urlValues.Add("key", ia.APIKey)
	urlObj.RawQuery = urlValues.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", urlObj.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform http request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request failed with status code: %d", resp.StatusCode)
	}

	respObj := new(IP2LocationIPInfoResponse)
	if err := json.NewDecoder(resp.Body).Decode(respObj); err != nil {
		return nil, fmt.Errorf("failed to decode ip2location http response: %v", err)
	}

	basicInfo := new(BasicIPInfo)
	if respObj.AS != nil && *respObj.AS != "" {
		basicInfo.ISP = *respObj.AS
	}
	if respObj.ASN != nil && *respObj.ASN != "" {
		basicInfo.ASN = *respObj.ASN
		if !strings.HasPrefix(basicInfo.ASN, "AS") {
			basicInfo.ASN = "AS" + basicInfo.ASN
		}
	}
	locations := make([]string, 0)
	if respObj.CityName != nil && *respObj.CityName != "" {
		locations = append(locations, *respObj.CityName)
		basicInfo.City = new(string)
		*basicInfo.City = *respObj.CityName
	}
	if respObj.RegionName != nil && *respObj.RegionName != "" {
		locations = append(locations, *respObj.RegionName)
		basicInfo.Region = new(string)
		*basicInfo.Region = *respObj.RegionName
	}
	if respObj.CountryName != nil && *respObj.CountryName != "" {
		locations = append(locations, *respObj.CountryName)
		basicInfo.Country = new(string)
		*basicInfo.Country = *respObj.CountryName
	}
	if len(locations) > 0 {
		basicInfo.Location = strings.Join(locations, ", ")
	}
	if respObj.Latitude != nil && respObj.Longitude != nil {
		basicInfo.Exact = &ExactLocation{
			Latitude:  *respObj.Latitude,
			Longitude: *respObj.Longitude,
		}
	}
	return basicInfo, nil
}

func (ia *IP2LocationIPInfoAdapter) GetName() string {
	return "ip2location"
}
