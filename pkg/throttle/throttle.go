package throttle

import (
	"context"
	"fmt"
	"time"
)

type TokenBasedThrottleConfig struct {
	RefreshInterval       time.Duration
	TokenQuotaPerInterval int
}

type RateLimiterToken struct {
	Quota        int
	BufferedChan chan interface{}
}

type TokenBasedThrottle struct {
	OutC   chan interface{}
	config TokenBasedThrottleConfig
}

func NewTokenBasedThrottle(config TokenBasedThrottleConfig) *TokenBasedThrottle {
	return &TokenBasedThrottle{
		config: config,
	}
}

func (tbThrottle *TokenBasedThrottle) Run(inChan <-chan interface{}) <-chan interface{} {
	outChan := make(chan interface{})

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	tokensChan := make(chan int, 1)
	tokensChan <- tbThrottle.config.TokenQuotaPerInterval

	// token generator goroutine
	go func() {
		ticker := time.NewTicker(tbThrottle.config.RefreshInterval)
		defer ticker.Stop()
		defer close(tokensChan)
		defer fmt.Println("[DBG] A1 closed")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tokensChan <- tbThrottle.config.TokenQuotaPerInterval
			}
		}
	}()

	// copying goroutine
	go func() {
		defer close(outChan)
		defer cancel()

		quota := 0
		for item := range inChan {
			if quota == 0 {
				quotaInc := <-tokensChan
				quota += quotaInc
				if quota > tbThrottle.config.TokenQuotaPerInterval {
					quota = tbThrottle.config.TokenQuotaPerInterval
				}
			}
			outChan <- item
			quota--
		}

		fmt.Println("[DBG] A2 closed")

	}()

	return outChan
}

type SpeedMeasurer struct {
	RefreshInterval   time.Duration
	PreviousCounter   int
	PreviousTimestamp time.Time
	MinTimeDelta      time.Duration
}

type SpeedRecord struct {
	Timestamp        time.Time
	Counter          int
	CounterIncrement int
	TimeDelta        time.Duration
}

func (sr *SpeedRecord) String() string {
	if sr.Value() == nil {
		return "<N/A>"
	}
	return fmt.Sprintf("%f pps", *sr.Value())
}

// the unit is pps (packets per second)
func (sr *SpeedRecord) Value() *float64 {
	if sr == nil || sr.TimeDelta == 0 {
		return nil
	}

	var v float64 = float64(sr.CounterIncrement) / sr.TimeDelta.Seconds()
	return &v
}

func (sm *SpeedMeasurer) Run(inChan <-chan interface{}) (<-chan interface{}, <-chan SpeedRecord) {
	sm.PreviousCounter = 0
	sm.PreviousTimestamp = time.Now()

	outChan := make(chan interface{})
	speedRecordChan := make(chan SpeedRecord, 1)

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	var counter *int = new(int)
	*counter = 0

	go func() {
		defer close(speedRecordChan)
		defer fmt.Println("[DBG] B1 closed")
		ticker := time.NewTicker(sm.RefreshInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				timeDelta := time.Since(sm.PreviousTimestamp)
				if timeDelta >= sm.MinTimeDelta {
					speedRecord := SpeedRecord{
						Timestamp:        time.Now(),
						Counter:          *counter,
						CounterIncrement: *counter - sm.PreviousCounter,
						TimeDelta:        timeDelta,
					}
					go func() {
						speedRecordChan <- speedRecord
					}()
				}
				sm.PreviousCounter = *counter
				sm.PreviousTimestamp = time.Now()
			}
		}
	}()

	go func() {
		defer close(outChan)
		defer cancel()
		defer fmt.Println("[DBG] B2 closed")

		for item := range inChan {
			outChan <- item
			*counter = *counter + 1
		}
	}()

	return outChan, speedRecordChan
}

type BurstSmoother struct {
	LeastSampleInterval time.Duration
}

func (bf *BurstSmoother) Run(inChan <-chan interface{}) <-chan interface{} {
	outChan := make(chan interface{})

	go func() {
		defer close(outChan)
		for item := range inChan {
			time.Sleep(bf.LeastSampleInterval)
			outChan <- item
		}
		fmt.Println("[DBG] C closed")
	}()

	return outChan
}
