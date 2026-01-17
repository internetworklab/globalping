package dnsprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"regexp"
	"time"
)

type Transport string

const (
	TransportUDP Transport = "udp"
	TransportTCP Transport = "tcp"
)

type DNSQueryType string

const (
	DNSQueryTypeA     DNSQueryType = "a"
	DNSQueryTypeAAAA  DNSQueryType = "aaaa"
	DNSQueryTypeCNAME DNSQueryType = "cname"
	DNSQueryTypeMX    DNSQueryType = "mx"
	DNSQueryTypeNS    DNSQueryType = "ns"
	DNSQueryTypePTR   DNSQueryType = "ptr"
	DNSQueryTypeTXT   DNSQueryType = "txt"
)

type LookupParameter struct {
	AddrPort  string       `json:"addrport"`
	Target    string       `json:"target"`
	TimeoutMs int64        `json:"timeoutMs"`
	Transport Transport    `json:"transport"`
	QueryType DNSQueryType `json:"queryType"`
}

type QueryResult struct {
	Server           string        `json:"server"`
	Target           string        `json:"target,omitempty"`
	QueryType        DNSQueryType  `json:"query_type,omitempty"`
	Answers          []interface{} `json:"answers,omitempty"`
	AnswerStrings    []string      `json:"answer_strings,omitempty"`
	Error            error         `json:"error,omitempty"`
	ErrString        string        `json:"err_string,omitempty"`
	IOTimeout        bool          `json:"io_timeout,omitempty"`
	NoSuchHost       bool          `json:"no_such_host,omitempty"`
	Elapsed          time.Duration `json:"elapsed,omitempty"`
	StartedAt        time.Time     `json:"started_at"`
	TimeoutSpecified time.Duration `json:"timeout_specified"`
}

// make it suitable for transmitting over the wire
func (qr *QueryResult) PreStringify() (*QueryResult, error) {
	clone := new(QueryResult)
	*clone = *qr
	if clone.Error != nil {
		clone.ErrString = clone.Error.Error()
		clone.Error = nil
	}
	if clone.Answers != nil {
		answerStrings := make([]string, 0)
		for _, ans := range clone.Answers {
			ansStr, err := wrappedAnsToString(ans, clone.QueryType)
			if err != nil {
				return nil, fmt.Errorf("failed to convert answer to string: %v", err)
			}
			answerStrings = append(answerStrings, ansStr)
		}
		clone.AnswerStrings = answerStrings
		clone.Answers = nil
	}
	return clone, nil
}

func analyzeError(err error, queryResult *QueryResult) bool {
	queryResult.Error = err
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		queryResult.IOTimeout = true
		return true
	} else if errors.Is(err, context.Canceled) {
		queryResult.IOTimeout = true
		return true
	} else if _, ok := err.(*net.DNSError); ok {
		queryResult.NoSuchHost = true
		return true
	} else {
		return false
	}
}

func appendPort53(s string) string {
	if !regexp.MustCompile(`:\d+$`).MatchString(s) {
		return net.JoinHostPort(s, "53")
	}
	return s
}

// returns: answers, error
func LookupDNS(ctx context.Context, parameter LookupParameter) (*QueryResult, error) {

	transport := parameter.Transport

	target := parameter.Target
	timeout := time.Duration(parameter.TimeoutMs) * time.Millisecond
	queryType := parameter.QueryType
	queryResult := new(QueryResult)
	queryResult.Target = target
	queryResult.QueryType = queryType
	queryResult.Answers = make([]interface{}, 0)
	queryResult.Server = parameter.AddrPort
	queryResult.TimeoutSpecified = timeout

	addrportObj, err := netip.ParseAddrPort(appendPort53(parameter.AddrPort))
	if err != nil {
		return nil, fmt.Errorf("failed to parse addrport %s as netip.AddrPort: %v", parameter.AddrPort, err)
	}

	queryResult.StartedAt = time.Now()
	defer func() {
		queryResult.Elapsed = time.Since(queryResult.StartedAt)
	}()

	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if transport == TransportUDP {
				udpaddr := net.UDPAddrFromAddrPort(addrportObj)
				if udpaddr == nil {
					return nil, fmt.Errorf("failed to get udpaddr from %s", addrportObj.String())
				}
				return net.DialUDP("udp", nil, udpaddr)
			} else if transport == TransportTCP {
				tcpaddr := net.TCPAddrFromAddrPort(addrportObj)
				if tcpaddr == nil {
					return nil, fmt.Errorf("failed to get tcpaddr from %s", addrportObj.String())
				}
				return net.DialTCP("tcp", nil, tcpaddr)
			} else {
				return nil, fmt.Errorf("transport is not specified or invalid transport: %s", transport)
			}
		},
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch queryType {
	case DNSQueryTypeA, DNSQueryTypeAAAA:
		ipPref := "ip"
		if queryType == DNSQueryTypeA {
			ipPref = "ip4"
		} else if queryType == DNSQueryTypeAAAA {
			ipPref = "ip6"
		}
		answers, err := resolver.LookupIP(ctx, ipPref, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup ip (type %s) for %s: %v", queryType, target, err)
		}

		for _, ans := range answers {
			queryResult.Answers = append(queryResult.Answers, ans)
		}
		return queryResult, nil
	case DNSQueryTypeCNAME:
		answer, err := resolver.LookupCNAME(ctx, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup ip (type %s) for %s: %v", queryType, target, err)
		}

		if answer != "" {
			queryResult.Answers = append(queryResult.Answers, answer)
		}
		return queryResult, nil
	case DNSQueryTypeMX:
		answers, err := resolver.LookupMX(ctx, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup mx for %s: %v", target, err)
		}
		for _, ans := range answers {
			if ans == nil {
				continue
			}
			queryResult.Answers = append(queryResult.Answers, ans)
		}
		return queryResult, nil
	case DNSQueryTypeNS:
		answers, err := resolver.LookupNS(ctx, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup ns for %s: %v", target, err)
		}
		for _, ans := range answers {
			if ans == nil {
				continue
			}
			queryResult.Answers = append(queryResult.Answers, ans)
		}
		return queryResult, nil
	case DNSQueryTypePTR:
		answer, err := resolver.LookupAddr(ctx, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup ptr for %s: %v", target, err)
		}
		for _, ans := range answer {
			if ans == "" {
				continue
			}
			queryResult.Answers = append(queryResult.Answers, ans)
		}
		return queryResult, nil
	case DNSQueryTypeTXT:
		answer, err := resolver.LookupTXT(ctx, target)
		if err != nil && !analyzeError(err, queryResult) {
			return nil, fmt.Errorf("failed to lookup txt for %s: %v", target, err)
		}
		for _, ans := range answer {
			queryResult.Answers = append(queryResult.Answers, ans)
		}
		return queryResult, nil
	default:
		return nil, fmt.Errorf("invalid query type: %s", queryType)
	}
}

func wrappedAnsToString(ans interface{}, qtype DNSQueryType) (string, error) {
	switch qtype {
	case DNSQueryTypeA:
		ip, ok := ans.(net.IP)
		if !ok {
			return "", fmt.Errorf("answer is not a net.IP: %v", ans)
		}
		return ip.String(), nil
	case DNSQueryTypeAAAA:
		ip, ok := ans.(net.IP)
		if !ok {
			return "", fmt.Errorf("answer is not a net.IP: %v", ans)
		}
		return ip.String(), nil
	case DNSQueryTypeCNAME:
		cname, ok := ans.(string)
		if !ok {
			return "", fmt.Errorf("answer is not a string: %v", ans)
		}
		return cname, nil
	case DNSQueryTypeMX:
		mx, ok := ans.(*net.MX)
		if !ok {
			return "", fmt.Errorf("answer is not a *net.MX: %v", ans)
		}
		return fmt.Sprintf("%s (pref=%d)", mx.Host, mx.Pref), nil
	case DNSQueryTypeNS:
		ns, ok := ans.(*net.NS)
		if !ok {
			return "", fmt.Errorf("answer is not a *net.NS: %v", ans)
		}
		return ns.Host, nil
	case DNSQueryTypePTR:
		ptr, ok := ans.(string)
		if !ok {
			return "", fmt.Errorf("answer is not a string: %v", ans)
		}
		return ptr, nil
	default:
		return "", fmt.Errorf("unknown query type: %s", qtype)
	}
}
