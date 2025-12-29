package pinger

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

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
	TTL                        TTLGenerator
	RandomPayloadSize          *int
	ResolveTimeoutMilliseconds *int
	IPInfoProviderName         *string

	// Currently, only 'icmp' is supported, and 'udp' is planned to be supported in the future
	L3PacketType *string
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
const ParamRandomPayloadSize = "randomPayloadSize"
const ParamDestination = "destination"
const ParamResolveTimeoutMilliseconds = "resolveTimeoutMilliseconds"
const ParamsIPInfoProviderName = "ipInfoProviderName"
const ParamL3PacketType = "l3PacketType"

const defaultTTL = 64

func ParseSimplePingRequest(r *http.Request) (*SimplePingRequest, error) {
	result := new(SimplePingRequest)

	if l3PacketType := r.URL.Query().Get(ParamL3PacketType); l3PacketType != "" {
		result.L3PacketType = &l3PacketType
	}

	if ipInfoProviderName := r.URL.Query().Get(ParamsIPInfoProviderName); ipInfoProviderName != "" {
		result.IPInfoProviderName = &ipInfoProviderName
	}

	if randomPayloadSize := r.URL.Query().Get(ParamRandomPayloadSize); randomPayloadSize != "" {
		randomPayloadSizeInt, err := strconv.Atoi(randomPayloadSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse random payload size: %v", err)
		}
		result.RandomPayloadSize = &randomPayloadSizeInt
	}

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
		if strings.HasPrefix(ttl, "auto(") || ttl == "auto" {
			autoTTL, err := ParseToAutoTTL(ttl)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ttl: %v", err)
			}
			result.TTL = autoTTL
		} else if strings.HasPrefix(ttl, "range(") {
			rangeTTL, err := ParseToRangeTTL(ttl)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ttl: %v", err)
			}
			result.TTL = rangeTTL
		} else {
			ints, err := pkgutils.ParseInts(ttl)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ttl: %v", err)
			}
			result.TTL = &RangeTTL{TTLs: ints}
		}
	} else {
		result.TTL = &RangeTTL{TTLs: []int{defaultTTL}}
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
		vals.Add(ParamTTL, pr.TTL.String())
	}
	if pr.ResolveTimeoutMilliseconds != nil {
		vals.Add(ParamResolveTimeoutMilliseconds, strconv.Itoa(*pr.ResolveTimeoutMilliseconds))
	}
	if pr.RandomPayloadSize != nil {
		vals.Add(ParamRandomPayloadSize, strconv.Itoa(*pr.RandomPayloadSize))
	}
	if pr.IPInfoProviderName != nil && *pr.IPInfoProviderName != "" {
		vals.Add(ParamsIPInfoProviderName, *pr.IPInfoProviderName)
	}
	if pr.L3PacketType != nil && *pr.L3PacketType != "" {
		vals.Add(ParamL3PacketType, *pr.L3PacketType)
	}

	return vals
}

type TTLGenerator interface {
	GetNext() int
	Reset()
	String() string
}

type AutoTTL struct {
	Start int
	Next  int
	lock  sync.Mutex `json:"-"`
}

const maxAllowedAutoTTL = 64

func ParseToAutoTTL(s string) (*AutoTTL, error) {
	if s == "auto" {
		return &AutoTTL{Start: 1, Next: 1}, nil
	}

	pattern1 := regexp.MustCompile(`^auto\((\d+)\)$`)
	if result := pattern1.FindStringSubmatch(s); result != nil {
		start, err := strconv.Atoi(result[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse auto ttl: %v", err)
		}
		return &AutoTTL{Start: start, Next: start}, nil
	}

	return nil, fmt.Errorf("failed to parse auto ttl: %s", s)
}

func (at *AutoTTL) GetNext() int {
	at.lock.Lock()
	defer at.lock.Unlock()

	retVal := at.Next
	at.Next++
	if at.Next > maxAllowedAutoTTL {
		at.Next = at.Start
	}

	return retVal
}

func (at *AutoTTL) Reset() {
	at.lock.Lock()
	defer at.lock.Unlock()

	at.Next = at.Start
}

func (at *AutoTTL) String() string {
	return fmt.Sprintf("auto(%d)", at.Start)
}

type RangeTTL struct {
	TTLs []int
	idx  int        `json:"-"`
	lock sync.Mutex `json:"-"`
}

func ParseToRangeTTL(s string) (*RangeTTL, error) {
	ints, err := pkgutils.ParseInts(s)
	if err != nil {
		return nil, fmt.Errorf("failed to parse range ttl: %v", err)
	}
	return &RangeTTL{TTLs: ints}, nil
}

func (rt *RangeTTL) GetNext() int {
	rt.lock.Lock()
	defer rt.lock.Unlock()

	retVal := rt.TTLs[rt.idx]
	rt.idx++
	if rt.idx >= len(rt.TTLs) {
		rt.idx = 0
	}

	return retVal
}

func (rt *RangeTTL) Reset() {
	rt.lock.Lock()
	defer rt.lock.Unlock()

	rt.idx = 0
}

func (rt *RangeTTL) String() string {
	segs := make([]string, 0)
	for _, ttl := range rt.TTLs {
		segs = append(segs, strconv.Itoa(ttl))
	}
	return strings.Join(segs, ",")
}
