package connreg

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
)

type RegisterPayload struct {
	NodeName string `json:"node_name"`
}

type EchoDirection string

const (
	EchoDirectionC2S EchoDirection = "ping"
	EchoDirectionS2C EchoDirection = "pong"
)

type EchoPayload struct {
	Direction       EchoDirection `json:"direction"`
	CorrelationID   string        `json:"correlation_id"`
	ServerTimestamp uint64        `json:"server_timestamp"`
	Timestamp       uint64        `json:"timestamp"`
	SeqID           uint64        `json:"seq_id"`
}

type AttributesAnnouncementPayload struct {
	Attributes  ConnectionAttributes `json:"attributes,omitempty"`
	Withdrawals []string             `json:"withdrawals,omitempty"`
}

func (echopayload *EchoPayload) CalculateDelays(now time.Time) (rtt time.Duration, onTrip time.Duration, backTrip time.Duration) {
	rtt = now.Sub(time.UnixMilli(int64(echopayload.Timestamp)))
	onTrip = time.UnixMilli(int64(echopayload.ServerTimestamp)).Sub(time.UnixMilli(int64(echopayload.Timestamp)))
	backTrip = now.Sub(time.UnixMilli(int64(echopayload.ServerTimestamp)))

	return rtt, onTrip, backTrip
}

type ConnectionAttributes map[string]string

type ConnRegistryData struct {
	NodeName      *string              `json:"node_name,omitempty"`
	ConnectedAt   uint64               `json:"connected_at"`
	RegisteredAt  *uint64              `json:"registered_at,omitempty"`
	LastHeartbeat *uint64              `json:"last_heartbeat,omitempty"`
	Attributes    ConnectionAttributes `json:"attributes,omitempty"`
}

func (regData *ConnRegistryData) Clone() *ConnRegistryData {
	return cloneConnRegistryData(regData).(*ConnRegistryData)
}

func cloneConnRegistryData(dataany interface{}) interface{} {
	data, ok := dataany.(*ConnRegistryData)
	if !ok {
		panic(fmt.Errorf("failed to convert dataany to *ConnRegistryData"))
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal connection registry data: %v", err)
		panic(err)
	}

	var cloned *ConnRegistryData
	err = json.Unmarshal(bytes, &cloned)
	if err != nil {
		panic(err)
	}
	return cloned
}

type ConnRegistry struct {
	datastore pkgsafemap.DataStore
}

func (cr *ConnRegistry) OpenConnection(key string) {
	now := uint64(time.Now().Unix())
	connRegData := &ConnRegistryData{
		ConnectedAt: now,
		Attributes:  make(ConnectionAttributes),
	}
	cr.datastore.Set(key, connRegData)
}

func (cr *ConnRegistry) CloseConnection(key string) {
	cr.datastore.Delete(key)
}

func (cr *ConnRegistry) Register(key string, payload RegisterPayload) error {
	log.Printf("Registering connection from %s, node name: %s", key, payload.NodeName)


	_, found := cr.datastore.Get(key, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		now := uint64(time.Now().Unix())
		if entry == nil {
			return fmt.Errorf("connection from %s not found in registry", key)
		}
		entry.NodeName = &payload.NodeName
		entry.RegisteredAt = &now
		return nil
	})

	if !found {
		return fmt.Errorf("connection from %s not found in registry", key)
	}
	return nil
}

func (cr *ConnRegistry) UpdateHeartbeat(key string) error {
	_, found := cr.datastore.Get(key, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		now := uint64(time.Now().Unix())
		entry.LastHeartbeat = &now

		return nil
	})

	if !found {
		return fmt.Errorf("connection from %s not found in registry", key)
	}
	return nil
}

func (cr *ConnRegistry) SetAttributes(connkey string, announcement *AttributesAnnouncementPayload) error {
	_, found := cr.datastore.Get(connkey, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		attrs := make(ConnectionAttributes)
		for k, v := range entry.Attributes {
			attrs[k] = v
		}
		for _, withdrawal := range announcement.Withdrawals {
			delete(attrs, withdrawal)
		}
		for k, v := range announcement.Attributes {
			attrs[k] = v
		}
		entry.Attributes = attrs
		return nil
	})
	if !found {
		return fmt.Errorf("connection from %s not found in registry", connkey)
	}
	return nil
}

func (cr *ConnRegistry) Dump() map[string]*ConnRegistryData {
	dummped := cr.datastore.Dump(cloneConnRegistryData)
	result := make(map[string]*ConnRegistryData)
	for k, v := range dummped {
		result[k] = v.(*ConnRegistryData)
	}
	return result
}

func (cr *ConnRegistry) Count() int {
	return cr.datastore.Len()
}

func NewConnRegistry(datastore pkgsafemap.DataStore) *ConnRegistry {
	connReg := &ConnRegistry{
		datastore: datastore,
	}
	return connReg
}

// If all matches, return true, otherwise return false
func (regData *ConnRegistryData) TestAgainstAttributes(expected ConnectionAttributes) (allMatch bool) {
	allMatch = true
	for k, v := range expected {
		actual, ok := regData.Attributes[k]
		if !ok {
			allMatch = false
			break
		}
		if actual != v {
			allMatch = false
			break
		}
	}
	return allMatch
}

func (cr *ConnRegistry) SearchByAttributes(expected ConnectionAttributes) (data *ConnRegistryData, err error) {
	err = cr.datastore.Walk(func(key string, value interface{}) (keepgoing bool, err error) {
		entry, ok := value.(*ConnRegistryData)
		if !ok {
			return false, fmt.Errorf("failed to convert value to *ConnRegistryData")
		}

		keepgoing = !entry.TestAgainstAttributes(expected)
		if !keepgoing {
			data = cloneConnRegistryData(entry).(*ConnRegistryData)
		}

		return keepgoing, nil
	})

	return data, err
}

func (cr *ConnRegistry) Shutdown(ctx context.Context) error {
	return nil
}
