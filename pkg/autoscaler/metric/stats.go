package metric

import (
	"fmt"
	"sync"
	"time"

	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type RequestStats struct {
	sync.Mutex
	Key                 string
	concurrency         float64
	concurrencyIntegral float64
	requestCount        float64
	lastChange          time.Time
	secondsInUse        float64
}

type RequestStatsReport struct {
	AverageConcurrency float64
	RequestCount       float64
}

func (r *RequestStatsReport) String() string {
	return fmt.Sprintf("AvgConcurrency: %.2f, ReqCount: %.0f", r.AverageConcurrency, r.RequestCount)
}

func NewRequestStats(key string) *RequestStats {
	return &RequestStats{Key: key}
}

func (s *RequestStats) Move(now time.Time) {
	if s.lastChange.IsZero() {
		s.lastChange = now
		return
	}
	if durationSinceChange := now.Sub(s.lastChange); durationSinceChange > 0 {
		durationSecs := durationSinceChange.Seconds()
		s.secondsInUse += durationSecs
		s.concurrencyIntegral += s.concurrency * durationSecs
		s.lastChange = now
	}
}

func (s *RequestStats) ReqIn(_ *workload.Request) float64 {
	s.Lock()
	defer s.Unlock()

	s.Move(time.Now())
	s.concurrency += 1
	s.requestCount += 1
	return s.concurrency
}

func (s *RequestStats) ReqOut(_ *workload.Response) float64 {
	s.Lock()
	defer s.Unlock()

	s.Move(time.Now())
	s.concurrency -= 1
	return s.concurrency
}

func (s *RequestStats) Report(now time.Time) *RequestStatsReport {
	s.Lock()
	defer s.Unlock()

	s.Move(now)
	defer s.reset()

	report := &RequestStatsReport{
		RequestCount: s.requestCount,
	}

	if s.secondsInUse > 0 {
		report.AverageConcurrency = s.concurrencyIntegral / s.secondsInUse
	}

	return report
}

func (s *RequestStats) InstantConcurrency() float64 {
	s.Lock()
	defer s.Unlock()
	return s.concurrency
}

func (s *RequestStats) reset() {
	s.concurrencyIntegral = 0
	s.requestCount = 0
	s.secondsInUse = 0
}
