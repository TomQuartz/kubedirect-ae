package metric

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	knas "knative.dev/serving/pkg/autoscaler/aggregation"
)

type Collector struct {
	*RequestStats
	concurrencyBuckets       *knas.TimedFloat64Buckets
	concurrencyPanicBuckets  *knas.TimedFloat64Buckets
	requestCountBuckets      *knas.TimedFloat64Buckets
	requestCountPanicBuckets *knas.TimedFloat64Buckets
	collectInterval          time.Duration
}

// granularity is bucket bin size, also the stats report interval
// the number of buckets if window/granularity
func NewCollector(key string, stableWindow, panicWindow, granularity time.Duration) *Collector {
	return &Collector{
		RequestStats:             NewRequestStats(key),
		concurrencyBuckets:       knas.NewTimedFloat64Buckets(stableWindow, granularity),
		concurrencyPanicBuckets:  knas.NewTimedFloat64Buckets(panicWindow, granularity),
		requestCountBuckets:      knas.NewTimedFloat64Buckets(stableWindow, granularity),
		requestCountPanicBuckets: knas.NewTimedFloat64Buckets(panicWindow, granularity),
		collectInterval:          granularity,
	}
}

func (c *Collector) collect(_ logr.Logger, now time.Time) {
	report := c.RequestStats.Report(now)
	// logger.V(1).Info("collecting metrics", "time", now, "report", report.String())
	c.concurrencyBuckets.Record(now, report.AverageConcurrency)
	c.concurrencyPanicBuckets.Record(now, report.AverageConcurrency)
	c.requestCountBuckets.Record(now, report.RequestCount)
	c.requestCountPanicBuckets.Record(now, report.RequestCount)
}

func (c *Collector) StableAndPanicConcurrency(now time.Time) (float64, float64) {
	return c.concurrencyBuckets.WindowAverage(now), c.concurrencyPanicBuckets.WindowAverage(now)
}

func (c *Collector) StableAndPanicAndInstantConcurrency(now time.Time) (float64, float64, float64) {
	return c.concurrencyBuckets.WindowAverage(now), c.concurrencyPanicBuckets.WindowAverage(now), c.InstantConcurrency()
}

func (c *Collector) StableAndPanicRequestCount(now time.Time) (float64, float64) {
	return c.requestCountBuckets.WindowAverage(now), c.requestCountPanicBuckets.WindowAverage(now)
}

func (c *Collector) Run(ctx context.Context) {
	logger := klog.FromContext(ctx).WithValues("src", "autoscaler/collector", "key", c.Key)
	logger.V(1).Info("Starting collector")
	ticker := time.NewTicker(c.collectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.collect(logger, now)
		}
	}
}
