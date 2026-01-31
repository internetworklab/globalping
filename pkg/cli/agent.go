package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkghandler "example.com/rbmq-demo/pkg/handler"
	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	pkgmyprom "example.com/rbmq-demo/pkg/myprom"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgratelimit "example.com/rbmq-demo/pkg/ratelimit"
	pkgraw "example.com/rbmq-demo/pkg/raw"
	pkgrouting "example.com/rbmq-demo/pkg/routing"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type AgentCmd struct {
	NodeName            string `help:"Nodename to advertise to the hub, leave it empty for not advertising itself to the hub"`
	HttpEndpoint        string `help:"HTTP endpoint to advertise to the hub"`
	ExactLocationLatLon string `help:"The exact geographic location to advertise to the hub, when present. Format: <latitude>,<longitude>"`
	CountryCode         string `help:"The country code to advertise to the hub, when present. Format: <iso3166-alpha2-country-code>"`
	CityName            string `help:"The city name to advertise to the hub, when present. Format: <name-of-the-city>"`
	ASN                 string `help:"The ASN of the ISP that provides internet connectivity to the node. Format: AS<number>, e.g. AS65001"`
	ISP                 string `help:"The name of the ISP that provides internet connectivity to the node"`
	DN42ASN             string `name:"dn42-asn" help:"The ASN of the ISP that provides DN42 connectivity to the node. Format: AS<number>, e.g. AS4242421234"`
	DN42ISP             string `name:"dn42-isp" help:"The name of the ISP that provides DN42 connectivity to the node"`

	// If server address is empty, it won't register itself to the hub.
	ServerAddress     string `help:"WebSocket Address of the hub" default:"wss://hub.example.com:8080/ws"`
	QUICServerAddress string `help:"QUIC Address of the hub"`

	RespondRange       []string `help:"A list of CIDR ranges defining what queries this agent will respond to, by default, all queries will be responded."`
	DomainRespondRange []string `help:"A domain respond range, when present, is a list of domain patterns that defines what queries will be responded in terms of domain name."`

	// PeerCAs are use to verify certs presented by the peer,
	// For agent, the peer is the hub, for hub, the peer is the agent.
	// Simply put, PeerCAs are what agent is use to verify the hub's cert, and what hub is use to verify the agent's cert.
	PeerCAs []string `help:"PeerCAs are custom CAs use to verify the hub (server)'s certificate, if none is provided, will use the system CAs to do so. PeerCAs are also use to verify the client's certificate when functioning as a server."`

	// Agent will connect to the hub (sometimes), so this is the TLS name (mostly CN field or DNS Alt Name) of the hub.
	ServerName string `help:"Also use to verify the server's certificate"`

	// When the agent is connecting to the hub, the hub needs to authenticate the client, so the client (the agent) also have to present a cert
	// to complete the m-TLS authentication process.
	ClientCert    string `help:"The path to the client certificate" type:"path"`
	ClientCertKey string `help:"The path to the client certificate key" type:"path"`

	// Agent also functions as a server (i.e. provides public tls-secured endpoint, so it might also needs a cert pair)
	ServerCert    string `help:"The path to the server certificate" type:"path"`
	ServerCertKey string `help:"The path to the server key" type:"path"`

	TLSListenAddress string `help:"Address to listen on for TLS" default:"localhost:8081"`

	// when http listen address is not empty, it will serve http requests without any TLS authentication
	HTTPListenAddress string `help:"Address to listen on for HTTP" default:""`

	SharedQuota                int    `help:"Shared quota for the traceroute (packets per second)" default:"10"`
	DN42IPInfoProvider         string `help:"APIEndpoint of DN42 IPInfo provider" default:"https://dn42-query.netneighbor.me/ipinfo/lite/query"`
	DN42IP2LocationAPIEndpoint string `help:"APIEndpoint of DN42 IP2Location provider"`

	// Prometheus stuffs
	MetricsListenAddress string `help:"Endpoint to expose prometheus metrics" default:":2112"`
	MetricsPath          string `help:"Path to expose prometheus metrics" default:"/metrics"`

	SupportUDP  bool `help:"Declare supportness for UDP traceroute" default:"false"`
	SupportPMTU bool `help:"Declare supportness for PMTU discovery" default:"false"`
	SupportTCP  bool `help:"Declare supportness for TCP-flavored ping" default:"false"`
	SupportDNS  bool `help:"Declare supportness for DNS probing" default:"false"`

	IPInfoCacheValiditySecs int `help:"The validity of the IPInfo cache in seconds" default:"600"`

	IP2LocationAPIEndpoint string `help:"APIEndpoint of IP2Location IPInfo provider" default:"https://api.ip2location.io/v2/"`

	AgentTickInterval string `help:"The interval between node registration agent's tick" default:"5s"`

	LogEchoReplies bool `help:"Log echo replies" default:"false"`

	SharedOutboundRateLimit                int    `name:"shared-outbound-ratelimit" help:"The shared outbound rate limit in packets per second" default:"100"`
	SharedOutboundRateLimitRefreshInterval string `name:"shared-outbound-ratelimit-refresh-interval" help:"The refresh interval of the shared outbound rate limit" default:"1s"`
}

type PingHandler struct {
	IPInfoReg          *pkgipinfo.IPInfoProviderRegistry
	RespondRange       []net.IPNet
	DomainRespondRange []regexp.Regexp
}

func getHostFromIP(ipstr string) (net.IP, error) {
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return nil, fmt.Errorf("failed to parse IP from string: %s", ipstr)
	}
	return ip, nil
}

func getHost(addrport string) (net.IP, error) {
	host, _, err := net.SplitHostPort(addrport)
	if err != nil {
		return getHostFromIP(addrport)
	}
	return getHostFromIP(host)
}

func (ph *PingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	pingRequest, err := pkgpinger.ParseSimplePingRequest(r)
	if err != nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
		return
	}

	pingReqJSB, _ := json.Marshal(pingRequest)
	log.Printf("Started ping request for %s: %s", pkgutils.GetRemoteAddr(r), string(pingReqJSB))
	defer log.Printf("Finished ping request for %s: %s", pkgutils.GetRemoteAddr(r), string(pingReqJSB))

	counterStore := r.Context().Value(pkgutils.CtxKeyPrometheusCounterStore).(*pkgmyprom.CounterStore)
	if counterStore == nil {
		panic("failed to obtain counter store from request context")
	}

	commonLabels := prometheus.Labels{
		pkgmyprom.PromLabelFrom:   strings.Join(pingRequest.From, ","),
		pkgmyprom.PromLabelTarget: pingRequest.Destination,
		pkgmyprom.PromLabelClient: pkgutils.GetRemoteAddr(r),
	}

	startedAt := time.Now()
	defer func() {
		servedDurationMs := time.Since(startedAt).Milliseconds()
		counterStore.NumRequestsServed.With(commonLabels).Add(1.0)
		counterStore.ServedDurationMs.With(commonLabels).Add(float64(servedDurationMs))
	}()

	if len(ph.DomainRespondRange) > 0 && net.ParseIP(pingRequest.Destination) == nil {
		hit := false
		for _, domainPattern := range ph.DomainRespondRange {
			if domainPattern.MatchString(pingRequest.Destination) {
				hit = true
				break
			}
		}
		if !hit {
			json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: fmt.Errorf("domain %s does not match any pattern in the domain respond range", pingRequest.Destination).Error()})
			return
		}
	}

	var ipinfoAdapter pkgipinfo.GeneralIPInfoAdapter = nil
	if pingRequest.IPInfoProviderName != nil && *pingRequest.IPInfoProviderName != "" {
		ipinfoAdapter, err = ph.IPInfoReg.GetAdapter(*pingRequest.IPInfoProviderName)
		if err != nil {
			json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
			return
		}
	}

	ctx := r.Context()
	var rateLimiterUsed pkgratelimit.RateLimiter = nil
	if rateLimitAny := ctx.Value(pkgutils.CtxKeySharedRateLimitEnforcer); rateLimitAny != nil {
		rateLimit, ok := rateLimitAny.(pkgratelimit.RateLimiter)
		if ok && rateLimit != nil {
			rateLimiterUsed = rateLimit
		}
	}

	var pinger pkgpinger.Pinger = nil
	if pingRequest.L7PacketType != nil && *pingRequest.L7PacketType == pkgpinger.L7ProtoDNS {
		dnsServers := make([]string, 0)
		dnsServerIPs := make([]net.IP, 0)
		for _, tgt := range pingRequest.DNSTargets {

			dnsServerIP, err := getHost(tgt.AddrPort)
			if err != nil {
				json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: fmt.Sprintf("failed to parse dns server ip from addrport: %s: %v", tgt.AddrPort, err)})
				return
			}
			dnsServerIPs = append(dnsServerIPs, dnsServerIP)
			dnsServers = append(dnsServers, tgt.AddrPort)
		}

		if len(ph.RespondRange) > 0 && !pkgutils.CheckIntersect(dnsServerIPs, ph.RespondRange) {
			json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: fmt.Errorf("dns server ips are not in the respond range").Error()})
			return
		}

		// when in dns mode, we are mainly sending packets to dns servers, so, set targets to dns servers
		commonLabels[pkgmyprom.PromLabelTarget] = strings.Join(dnsServers, ",")

		pinger = &pkgpinger.DNSPinger{
			Requests:    pingRequest.DNSTargets,
			RateLimiter: rateLimiterUsed,
		}
	} else if pingRequest.L4PacketType != nil && *pingRequest.L4PacketType == pkgpinger.L4ProtoTCP {
		tcpingPinger := &pkgpinger.TCPSYNPinger{
			PingRequest:   pingRequest,
			IPInfoAdapter: ipinfoAdapter,
			RespondRange:  ph.RespondRange,
			OnSent: func(ctx context.Context, srcIP net.IP, srcPort int, dstIP net.IP, dstPort int, nBytes int) {
				counterStore.NumBytesSent.With(commonLabels).Add(float64(nBytes))
			},
			OnReceived: func(ctx context.Context, srcIP net.IP, srcPort int, dstIP net.IP, dstPort int, nBytes int) {
				counterStore.NumBytesReceived.With(commonLabels).Add(float64(nBytes))
			},
			RateLimiter: rateLimiterUsed,
		}

		pinger = tcpingPinger
	} else {
		icmpOrUDPPinger := &pkgpinger.SimplePinger{
			PingRequest:   pingRequest,
			IPInfoAdapter: ipinfoAdapter,
			RespondRange:  ph.RespondRange,
			OnSent: func(ctx context.Context, request *pkgraw.ICMPSendRequest, reply *pkgraw.ICMPReceiveReply, peer string, nBytes int) error {
				counterStore.NumBytesSent.With(commonLabels).Add(float64(nBytes))
				return nil
			},
			OnReceived: func(ctx context.Context, request *pkgraw.ICMPSendRequest, reply *pkgraw.ICMPReceiveReply, peer string, nBytes int) error {
				counterStore.NumBytesReceived.With(commonLabels).Add(float64(nBytes))
				return nil
			},
			RateLimiter: rateLimiterUsed,
		}
		pinger = icmpOrUDPPinger
	}

	ctx = context.WithValue(ctx, pkgutils.CtxKeyPromCommonLabels, commonLabels)
	for ev := range pinger.Ping(ctx) {
		if ev.Error != nil {
			errStr := ev.Error.Error()
			ev.Err = &errStr
		}
		if err := json.NewEncoder(w).Encode(ev); err != nil {
			log.Printf("failed to serialize and send an event: %v", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func getClearnetIPInfoAdapter(agentCmd *AgentCmd) (pkgipinfo.GeneralIPInfoAdapter, error) {
	ip2LocationEndpoint := agentCmd.IP2LocationAPIEndpoint
	ip2LocationAPIKey := os.Getenv("IP2LOCATION_API_KEY")
	if ip2LocationEndpoint != "" && ip2LocationAPIKey != "" {
		log.Printf("Using IP2Location API Service: %s", ip2LocationEndpoint)
		ip2LocationIPInfoAdapter := pkgipinfo.NewIP2LocationIPInfoAdapter(ip2LocationEndpoint, ip2LocationAPIKey, "ip2location")
		return ip2LocationIPInfoAdapter, nil
	}

	ipinfoLiteToken := os.Getenv("IPINFO_TOKEN")
	if ipinfoLiteToken != "" {
		log.Printf("Using IPInfo Lite API Service: %s", ipinfoLiteToken)
		ipinfoLiteIPInfoAdapter, err := pkgipinfo.NewIPInfoAdapter(&ipinfoLiteToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create IPInfo Lite adapter: %v", err)
		}
		return ipinfoLiteIPInfoAdapter, nil
	}

	return nil, fmt.Errorf("no valid ipinfo provider found")
}

func getDN42IPInfoAdapter(agentCmd *AgentCmd) (pkgipinfo.GeneralIPInfoAdapter, error) {
	if agentCmd.DN42IP2LocationAPIEndpoint != "" {
		return pkgipinfo.NewIP2LocationIPInfoAdapter(agentCmd.DN42IP2LocationAPIEndpoint, "", "dn42"), nil
	}

	return pkgipinfo.NewDN42IPInfoAdapter(agentCmd.DN42IPInfoProvider), nil
}

const defaultTickInterval = 5 * time.Second
const minTickInterval = 1000 * time.Millisecond

func (agentCmd *AgentCmd) Run(sharedCtx *pkgutils.GlobalSharedContext) error {

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rlRefreshIntv, err := time.ParseDuration(agentCmd.SharedOutboundRateLimitRefreshInterval)
	if err != nil {
		log.Fatalf("failed to parse shared outbound rate limit refresh interval: %v", err)
	}

	sharedRateLimitPool := &pkgratelimit.MemoryBasedRateLimitPool{
		RefreshIntv:     rlRefreshIntv,
		NumTokensPerKey: agentCmd.SharedOutboundRateLimit,
	}
	sharedRateLimitPool.Run(ctx)

	sharedRateLimitEnforcer := &pkgratelimit.MemoryBasedRateLimiter{
		Pool:   sharedRateLimitPool,
		GetKey: pkgratelimit.GlobalKeyFunc,
	}

	log.Printf("Using bucket-based token rate limiter, with %d tokens per slot, refreshing every %s", agentCmd.SharedOutboundRateLimit, rlRefreshIntv.String())

	counterStore := pkgmyprom.NewCounterStore()
	ctx = context.WithValue(ctx, pkgutils.CtxKeyPrometheusCounterStore, counterStore)

	counterStore.StartedTime.Set(float64(time.Now().Unix()))

	ipinfoReg := pkgipinfo.NewIPInfoProviderRegistry()

	classicIPInfoAdapter, err := getClearnetIPInfoAdapter(agentCmd)
	if err != nil {
		log.Fatalf("failed to initialize IPInfo adapter: %v", err)
	}
	// skip registering named ipinfo providers to the registry,
	// to prevent users from intentionally bypassing the (cached) auto ipinfo dispatcher.
	// ipinfoReg.RegisterAdapter(classicIPInfoAdapter)
	dn42IPInfoAdapter, err := getDN42IPInfoAdapter(agentCmd)
	if err != nil {
		log.Fatalf("failed to initialize DN42 IPInfo adapter: %v", err)
	}
	// ipinfoReg.RegisterAdapter(dn42IPInfoAdapter)
	randomIPInfoAdapter := pkgipinfo.NewRandomIPInfoAdapter()
	ipinfoReg.RegisterAdapter(randomIPInfoAdapter)

	autoIPInfoDispatcher := &pkgipinfo.AutoIPInfoDispatcher{
		Router: pkgrouting.NewSimpleRouter(),
	}
	autoIPInfoDispatcher.SetUpDefaultRoutes(dn42IPInfoAdapter, classicIPInfoAdapter)

	ipinfoCacheHook := func(ctx context.Context, stats pkgipinfo.IPInfoRequestStats) {
		// will remove this logging code later

		commonLabels := ctx.Value(pkgutils.CtxKeyPromCommonLabels).(prometheus.Labels)
		counterStore.IPInfoServedDurationMs.With(commonLabels).Add(stats.DurationMs)
		ipinfoRequestLabels := maps.Clone(commonLabels)
		ipinfoRequestLabels[pkgmyprom.PromLabelCacheHit] = strconv.FormatBool(stats.CacheHit)
		ipinfoRequestLabels[pkgmyprom.PromLabelHasError] = strconv.FormatBool(stats.HasError)
		counterStore.IPInfoRequests.With(ipinfoRequestLabels).Add(1.0)
	}
	ipinfoCacheValidity := time.Duration(agentCmd.IPInfoCacheValiditySecs) * time.Second
	log.Printf("IPInfo cache validity: %s", ipinfoCacheValidity.String())
	cachedAutoIPInfoDispatcher := pkgipinfo.NewCacheIPInfoProvider(autoIPInfoDispatcher, ipinfoCacheValidity, ipinfoCacheHook)
	cachedAutoIPInfoDispatcher.Run(ctx)
	ipinfoReg.RegisterAdapter(cachedAutoIPInfoDispatcher)

	customCAs, err := pkgutils.NewCustomCAPool(agentCmd.PeerCAs)
	if err != nil {
		log.Fatalf("Failed to create custom CA pool: %v", err)
	} else if len(agentCmd.PeerCAs) > 0 {
		log.Printf("Appended custom CAs: %s", strings.Join(agentCmd.PeerCAs, ", "))
	}

	if agentCmd.SharedQuota < 1 {
		log.Fatalf("shared quota must be greater than 0")
	}

	respondRangeNet := make([]net.IPNet, 0)
	for _, rangeStr := range agentCmd.RespondRange {
		_, nw, err := net.ParseCIDR(rangeStr)
		if err != nil {
			log.Fatalf("failed to parse respond range %s: %v", rangeStr, err)
		}
		respondRangeNet = append(respondRangeNet, *nw)
	}

	domaonRespondRange := make([]regexp.Regexp, 0)
	for _, domainPattern := range agentCmd.DomainRespondRange {
		domainRegexp, err := regexp.Compile(domainPattern)
		if err != nil {
			log.Fatalf("failed to compile domain pattern %s: %v", domainPattern, err)
		}
		domaonRespondRange = append(domaonRespondRange, *domainRegexp)
	}

	handler := &PingHandler{
		IPInfoReg:          ipinfoReg,
		RespondRange:       respondRangeNet,
		DomainRespondRange: domaonRespondRange,
	}

	muxer := http.NewServeMux()
	muxer.Handle("/simpleping", handler)
	muxer.Handle("/tcping", handler)
	muxer.Handle("/dnsprobe", handler)
	muxer.Handle("/version", pkghandler.NewVersionHandler(sharedCtx))

	var muxedHandler http.Handler = muxer
	muxedHandler = pkgmyprom.WithCounterStoreHandler(muxedHandler, counterStore)
	muxedHandler = pkgratelimit.WithRatelimiters(muxedHandler, sharedRateLimitPool, sharedRateLimitEnforcer)

	// TLSConfig to apply when acting as a server (i.e. we provide services, peer calls us)
	serverSideTLSCfg := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	if customCAs != nil {
		serverSideTLSCfg.ClientCAs = customCAs
	}

	if agentCmd.ServerCert != "" && agentCmd.ServerCertKey != "" {
		cert, err := tls.LoadX509KeyPair(agentCmd.ServerCert, agentCmd.ServerCertKey)
		if err != nil {
			log.Fatalf("failed to load server certificate: %v", err)
		}
		if serverSideTLSCfg.Certificates == nil {
			serverSideTLSCfg.Certificates = make([]tls.Certificate, 0)
		}
		serverSideTLSCfg.Certificates = append(serverSideTLSCfg.Certificates, cert)
		log.Printf("Loaded server certificate: %s and key: %s", agentCmd.ServerCert, agentCmd.ServerCertKey)
	}

	prometheusListener, err := net.Listen("tcp", agentCmd.MetricsListenAddress)
	if err != nil {
		log.Fatalf("failed to listen on address for prometheus metrics: %s: %v", agentCmd.MetricsListenAddress, err)
	}
	log.Printf("Listening on address %s for prometheus metrics", agentCmd.MetricsListenAddress)

	go func() {
		log.Printf("Serving prometheus metrics on address %s", prometheusListener.Addr())
		handler := promhttp.Handler()
		serveMux := http.NewServeMux()
		serveMux.Handle(agentCmd.MetricsPath, handler)
		server := http.Server{
			Handler: serveMux,
		}
		if err := server.Serve(prometheusListener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Fatalf("failed to serve prometheus metrics: %v", err)
			}
			log.Println("Prometheus metrics server exitted")
		}
	}()

	listener, err := tls.Listen("tcp", agentCmd.TLSListenAddress, serverSideTLSCfg)
	if err != nil {
		log.Fatalf("failed to listen on address %s: %v", agentCmd.TLSListenAddress, err)
	}
	defer listener.Close()
	log.Printf("Listening on address %s", agentCmd.TLSListenAddress)

	go func() {
		server := http.Server{
			Handler:   muxedHandler,
			TLSConfig: serverSideTLSCfg,
		}
		log.Printf("Serving HTTPS requests on address %s", listener.Addr())
		if err := server.Serve(listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Fatalf("failed to serve: %v", err)
			}
			log.Println("Server exitted")
		}
		go func() {
			<-ctx.Done()
			log.Println("Shutting down server")
			server.Shutdown(ctx)
		}()
	}()

	if agentCmd.HTTPListenAddress != "" {
		listener, err := net.Listen("tcp", agentCmd.HTTPListenAddress)
		if err != nil {
			log.Fatalf("failed to listen on address %s: %v", agentCmd.HTTPListenAddress, err)
		}
		defer listener.Close()
		log.Printf("Listening on address %s", agentCmd.HTTPListenAddress)
		go func() {
			log.Printf("Serving HTTP requests on address %s", listener.Addr())
			server := &http.Server{
				Handler: muxedHandler,
			}
			if err := server.Serve(listener); err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Fatalf("failed to serve: %v", err)
				}
				log.Println("Server exitted")
			}
			go func() {
				<-ctx.Done()
				log.Println("Shutting down server")
				server.Shutdown(ctx)
			}()
		}()
	}

	if agentCmd.NodeName != "" && agentCmd.ServerAddress != "" {
		log.Printf("Will advertise self as: %s endpoint: %s hub: %s", agentCmd.NodeName, agentCmd.HttpEndpoint, agentCmd.ServerAddress)
		attributes := make(pkgconnreg.ConnectionAttributes)
		attributes[pkgnodereg.AttributeKeyPingCapability] = "true"
		attributes[pkgnodereg.AttributeKeyNodeName] = agentCmd.NodeName
		attributes[pkgnodereg.AttributeKeyHttpEndpoint] = agentCmd.HttpEndpoint
		if agentCmd.ExactLocationLatLon != "" {
			attributes[pkgnodereg.AttributeKeyExactLocation] = agentCmd.ExactLocationLatLon
		}

		if alpha2 := agentCmd.CountryCode; alpha2 != "" {
			attributes[pkgnodereg.AttributeKeyCountryCode] = alpha2
		}

		if city := agentCmd.CityName; city != "" {
			attributes[pkgnodereg.AttributeKeyCityName] = city
		}

		if asn := agentCmd.ASN; asn != "" {
			attributes[pkgnodereg.AttributeKeyASN] = asn
		}

		if isp := agentCmd.ISP; isp != "" {
			attributes[pkgnodereg.AttributeKeyISP] = isp
		}

		if dn42asn := agentCmd.DN42ASN; dn42asn != "" {
			attributes[pkgnodereg.AttributeKeyDN42ASN] = dn42asn
		}

		if dn42isp := agentCmd.DN42ISP; dn42isp != "" {
			attributes[pkgnodereg.AttributeKeyDN42ISP] = dn42isp
		}

		if len(agentCmd.RespondRange) > 0 {
			attributes[pkgnodereg.AttributeKeyRespondRange] = strings.Join(agentCmd.RespondRange, ",")
		}

		if len(agentCmd.DomainRespondRange) > 0 {
			// the domain respond range involved complex regex string literals, so better encode it somehow before transmitting it over the wire.
			rangesJsonB, err := json.Marshal(agentCmd.DomainRespondRange)
			if err != nil {
				log.Fatalf("failed to marshal domain respond range: %v", err)
			}
			attributes[pkgnodereg.AttributeKeyDomainRespondRange] = string(rangesJsonB)
		}

		if agentCmd.SupportUDP {
			attributes[pkgnodereg.AttributeKeySupportUDP] = "true"
		}

		if agentCmd.SupportPMTU {
			attributes[pkgnodereg.AttributeKeySupportPMTU] = "true"
		}

		if agentCmd.SupportTCP {
			attributes[pkgnodereg.AttributeKeySupportTCP] = "true"
		}

		if agentCmd.SupportDNS {
			attributes[pkgnodereg.AttributeKeyDNSProbeCapability] = "true"
		}

		if agentCmd.QUICServerAddress != "" {
			attributes[pkgnodereg.AttributeKeySupportQUICTunnel] = "true"
		}

		versionJ, _ := json.Marshal(sharedCtx.BuildVersion)
		attributes[pkgnodereg.AttributeKeyVersion] = string(versionJ)

		agent := pkgnodereg.NodeRegistrationAgent{
			HTTPMuxer:         muxedHandler,
			ServerAddress:     agentCmd.ServerAddress,
			QUICServerAddress: agentCmd.QUICServerAddress,
			UseQUIC:           agentCmd.QUICServerAddress != "",
			NodeName:          agentCmd.NodeName,
			ClientCert:        agentCmd.ClientCert,
			ClientCertKey:     agentCmd.ClientCertKey,
			TickInterval:      defaultTickInterval,
			LogEchoReplies:    agentCmd.LogEchoReplies,
		}

		if customTickIntv := agentCmd.AgentTickInterval; customTickIntv != "" {
			intv, err := time.ParseDuration(customTickIntv)
			if err == nil && int64(intv) >= int64(minTickInterval) {
				agent.TickInterval = intv
			}
		}

		agent.NodeAttributes = attributes
		log.Println("Node attributes will be announced as:", attributes)

		log.Println("Initializing node registration agent...")
		if err = agent.Init(); err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}

		log.Println("Starting node registration agent...")

		agent.CustomCertPool = customCAs
		agent.ServerName = agentCmd.ServerName
		go func() {
			for {
				nodeRegAgentErrCh := agent.Run(ctx)
				if err, ok := <-nodeRegAgentErrCh; ok && err != nil {
					log.Printf("Node registration agent exited with error: %v, restarting...", err)
					time.Sleep(3 * time.Second)
					continue
				}
				log.Println("Node registration agent exited normally")
				return
			}

		}()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("Received signal: %v, exiting...", sig.String())
	cancel()

	return nil
}
