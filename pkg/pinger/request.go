package pinger

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type SimplePingRequest struct {
	From                       []string
	Destination                string
	Targets                    []string
	IntvMilliseconds           int
	PktTimeoutMilliseconds     int
	PreferV4                   *bool
	PreferV6                   *bool
	TotalPkts                  *int
	Resolver                   *string
	TTL                        []int
	ResolveTimeoutMilliseconds *int
}

const ParamTargets = "targets"
const ParamFrom = "from"
const ParamCount = "count"
const ParamIntvMs = "intervalMs"
const ParamPktTimeoutMs = "pktTimeoutMs"
const ParamTTL = "ttl"
const ParamPreferV4 = "preferV4"
const ParamPreferV6 = "preferV6"
const ParamResolver = "resolver"

const ParamDestination = "destination"
const ParamResolveTimeoutMilliseconds = "resolveTimeoutMilliseconds"

const defaultTTL = 64

func ParseSimplePingRequest(r *http.Request) (*SimplePingRequest, error) {
	result := new(SimplePingRequest)
	if targetsStr := r.URL.Query().Get(ParamTargets); targetsStr != "" {
		targets := strings.Split(targetsStr, ",")
		for _, target := range targets {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			result.Targets = append(result.Targets, target)
		}
	}

	if fromStr := r.URL.Query().Get(ParamFrom); fromStr != "" {
		froms := strings.Split(fromStr, ",")
		for _, from := range froms {
			from = strings.TrimSpace(from)
			if from == "" {
				continue
			}
			result.From = append(result.From, from)
		}
	}

	if count := r.URL.Query().Get(ParamCount); count != "" {
		countInt, err := strconv.Atoi(count)
		if err != nil {
			return nil, fmt.Errorf("failed to parse count: %v", err)
		}
		result.TotalPkts = &countInt
	}

	if intervalMilliSecs := r.URL.Query().Get(ParamIntvMs); intervalMilliSecs != "" {
		intervalInt, err := strconv.Atoi(intervalMilliSecs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse interval: %v", err)
		}
		result.IntvMilliseconds = intervalInt
	} else {
		result.IntvMilliseconds = 1000
	}

	if pktTimeoutMilliSecs := r.URL.Query().Get(ParamPktTimeoutMs); pktTimeoutMilliSecs != "" {
		pktTimeoutInt, err := strconv.Atoi(pktTimeoutMilliSecs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pktTimeout: %v", err)
		}
		result.PktTimeoutMilliseconds = pktTimeoutInt
	} else {
		result.PktTimeoutMilliseconds = 3000
	}

	if ttl := r.URL.Query().Get(ParamTTL); ttl != "" {

		ttls, err := pkgutils.ParseInts(ttl)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ttl: %v", err)
		}
		result.TTL = ttls
	} else {
		result.TTL = []int{defaultTTL}
	}

	if preferV4 := r.URL.Query().Get(ParamPreferV4); preferV4 != "" {
		preferV4Bool, err := strconv.ParseBool(preferV4)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preferV4: %v", err)
		}
		result.PreferV4 = &preferV4Bool
	}

	if preferV6 := r.URL.Query().Get(ParamPreferV6); preferV6 != "" {
		preferV6Bool, err := strconv.ParseBool(preferV6)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preferV6: %v", err)
		}
		result.PreferV6 = &preferV6Bool
	}

	if resolver := r.URL.Query().Get(ParamResolver); resolver != "" {
		result.Resolver = &resolver
	}

	destination := r.URL.Query().Get(ParamDestination)
	if destination == "" {
		if len(result.Targets) == 0 {
			return nil, fmt.Errorf("destination is required")
		}
		destination = result.Targets[0]
	}
	result.Destination = destination

	return result, nil
}

func (pr *SimplePingRequest) ToURLValues() url.Values {
	vals := url.Values{}

	if pr.Targets != nil {
		vals.Add(ParamTargets, strings.Join(pr.Targets, ","))
	}

	if pr.From != nil {
		vals.Add(ParamFrom, strings.Join(pr.From, ","))
	}

	vals.Add(ParamDestination, pr.Destination)
	vals.Add(ParamIntvMs, strconv.Itoa(pr.IntvMilliseconds))
	vals.Add(ParamPktTimeoutMs, strconv.Itoa(pr.PktTimeoutMilliseconds))
	if pr.PreferV4 != nil {
		vals.Add(ParamPreferV4, strconv.FormatBool(*pr.PreferV4))
	}
	if pr.PreferV6 != nil {
		vals.Add(ParamPreferV6, strconv.FormatBool(*pr.PreferV6))
	}
	if pr.TotalPkts != nil {
		vals.Add(ParamCount, strconv.Itoa(*pr.TotalPkts))
	}
	if pr.Resolver != nil {
		vals.Add(ParamResolver, *pr.Resolver)
	}
	if pr.TTL != nil {
		ttls := make([]string, 0)
		for _, ttl := range pr.TTL {
			ttls = append(ttls, strconv.Itoa(ttl))
		}
		vals.Add(ParamTTL, strings.Join(ttls, ","))
	}
	if pr.ResolveTimeoutMilliseconds != nil {
		vals.Add(ParamResolveTimeoutMilliseconds, strconv.Itoa(*pr.ResolveTimeoutMilliseconds))
	}

	return vals
}
