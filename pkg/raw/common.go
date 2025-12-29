package raw

import (
	"fmt"
	"net"

	"context"

	pkgmyprom "example.com/rbmq-demo/pkg/myprom"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sys/unix"
)

func getMaximumMTU() int {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	maximumMTU := -1
	for _, iface := range ifaces {
		if iface.MTU > maximumMTU {
			maximumMTU = iface.MTU
		}
	}
	if maximumMTU == -1 {
		panic("can't determine maximum MTU")
	}
	return maximumMTU
}

// setDFBit sets the Don't Fragment (DF) bit on the IPv4 packet connection
// by setting the IP_MTU_DISCOVER socket option to IP_PMTUDISC_DO.
// This ensures that routers will send ICMP errors instead of fragmenting packets.
func setDFBit(conn net.PacketConn) error {
	// Get the underlying syscall.RawConn
	rawConn, err := conn.(*net.IPConn).SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get raw connection: %v", err)
	}

	var setErr error
	err = rawConn.Control(func(fd uintptr) {
		// Set IP_MTU_DISCOVER to IP_PMTUDISC_DO to enable DF bit
		// IP_PMTUDISC_DO = 2 means "Always set DF"
		setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MTU_DISCOVER, unix.IP_PMTUDISC_DO)
	})
	if err != nil {
		return fmt.Errorf("failed to control raw connection: %v", err)
	}
	if setErr != nil {
		return fmt.Errorf("failed to set DF bit: %v", setErr)
	}
	return nil
}

func markAsSentBytes(ctx context.Context, n int) {
	commonLabels := ctx.Value(pkgutils.CtxKeyPromCommonLabels).(prometheus.Labels)
	if commonLabels == nil {
		panic("commonLabels is nil")
	}

	counterStore := ctx.Value(pkgutils.CtxKeyPrometheusCounterStore).(*pkgmyprom.CounterStore)
	if counterStore == nil {
		panic("counterStore is nil")
	}
	counterStore.NumBytesSent.With(commonLabels).Add(float64(n))
}

func markAsReceivedBytes(ctx context.Context, n int) {
	commonLabels := ctx.Value(pkgutils.CtxKeyPromCommonLabels).(prometheus.Labels)
	if commonLabels == nil {
		panic("commonLabels is nil")
	}
	counterStore := ctx.Value(pkgutils.CtxKeyPrometheusCounterStore).(*pkgmyprom.CounterStore)
	if counterStore == nil {
		panic("counterStore is nil")
	}
	counterStore.NumBytesReceived.With(commonLabels).Add(float64(n))
}
