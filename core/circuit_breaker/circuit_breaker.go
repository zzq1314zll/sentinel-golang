package circuit_breaker

import (
	"sync/atomic"
	"time"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/stat"
	"github.com/alibaba/sentinel-golang/util"
)

type CircuitBreaker interface {
	getRule() Rule
	TryPass(ctx *base.EntryContext) bool
}

// average rt circuit breaker will cut resource if the rt of resource exceed the threshold of rule.
type averageRtCircuitBreaker struct {
	// status of the circuit breaker
	cut util.AtomicBool
	// the count of request exceed the threshold
	passCount int64
	rule      *averageRtRule
	metric    base.ReadStat
}

func newAverageRtCircuitBreaker(rule *averageRtRule) *averageRtCircuitBreaker {
	resNode := stat.GetResourceNode(rule.Resource)
	var metric base.ReadStat
	// TODO need to optimize, we should to handle the scenario that resNode is nil
	if resNode != nil {
		metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
	}
	return &averageRtCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

// For test
func newAverageRtCircuitBreakerWithMetric(rule *averageRtRule, metric base.ReadStat) *averageRtCircuitBreaker {
	return &averageRtCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

func (b averageRtCircuitBreaker) getRule() Rule {
	return b.rule
}

func (b *averageRtCircuitBreaker) TryPass(_ *base.EntryContext) bool {
	// currently, the breaker is before auto recover, direct return blocked .
	if b.cut.Get() {
		return false
	}
	rule := b.rule
	if rule == nil {
		return true
	}

	// TODO need to optimize here.
	// We might create individual stat structures for circuit breakers, rather than use the universal ResourceNode.
	if b.metric == nil {
		resNode := stat.GetResourceNode(rule.Resource)
		if resNode == nil {
			logger.Errorf("Resource(%s)'s stat node is nil.", rule.Resource)
			return true
		}
		b.metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
		logger.Errorf("Delayed to initialize the metric of averageRtCircuitBreaker.")
	}

	avgRt := b.metric.AvgRT()
	if avgRt < rule.Threshold {
		atomic.StoreInt64(&b.passCount, 0)
		return true
	}
	if util.IncrementAndGetInt64(&b.passCount) < rule.RtSlowRequestAmount {
		return true
	}
	// trigger circuit breaker
	if b.cut.CompareAndSet(false, true) {
		go util.RunWithRecover(func() {
			// recover after RecoverTimeout seconds
			time.Sleep(time.Second * time.Duration(rule.RecoverTimeout))
			atomic.StoreInt64(&b.passCount, 0)
			b.cut.Set(false)
		}, logger)
	}
	return false
}

// error ratio circuit breaker will cut resource if the error ratio of resource exceed the threshold of rule.
type errorRatioCircuitBreaker struct {
	// status of the breaker
	cut util.AtomicBool
	// the count of request exceed the threshold
	passCount int64
	rule      *errorRatioRule
	metric    base.ReadStat
}

func newErrorRatioCircuitBreaker(rule *errorRatioRule) *errorRatioCircuitBreaker {
	resNode := stat.GetResourceNode(rule.Resource)
	var metric base.ReadStat
	// TODO need to optimize, we should to handle the scenario that resNode is nil
	if resNode != nil {
		metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
	}
	return &errorRatioCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

func newErrorRatioCircuitBreakerWithMetric(rule *errorRatioRule, metric base.ReadStat) *errorRatioCircuitBreaker {
	return &errorRatioCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

func (b *errorRatioCircuitBreaker) getRule() Rule {
	return b.rule
}

func (b *errorRatioCircuitBreaker) TryPass(_ *base.EntryContext) bool {
	if b.cut.Get() {
		return false
	}

	rule := b.rule
	if rule == nil {
		return true
	}

	// TODO need to optimize here.
	// We might create individual stat structures for circuit breakers, rather than use the universal ResourceNode.
	if b.metric == nil {
		resNode := stat.GetResourceNode(rule.Resource)
		if resNode == nil {
			logger.Errorf("Resource(%s)'s stat node is nil.", rule.Resource)
			return true
		}
		b.metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
		logger.Errorf("Delayed to initialize the metric of errorRatioCircuitBreaker.")
	}

	// biz error total
	err := b.metric.GetQPS(base.MetricEventError)
	// complete = err +  realComplete
	complete := b.metric.GetQPS(base.MetricEventComplete)
	// total = pass + blocked
	total := b.metric.GetQPS(base.MetricEventPass) + b.metric.GetQPS(base.MetricEventBlock)

	// If total amount is less than minRequestAmount, the request will pass.
	if total < float64(rule.MinRequestAmount) {
		return true
	}

	// "success" (aka. completed count) = error count + non-error count (realComplete)
	realComplete := complete - err
	// error count
	if realComplete <= 0 && err < float64(rule.MinRequestAmount) {
		return true
	}

	// err/complete is error ratio of the biz
	if err/complete < rule.Threshold {
		return true
	}

	if b.cut.CompareAndSet(false, true) {
		go util.RunWithRecover(func() {
			// recover after RecoverTimeout seconds
			time.Sleep(time.Second * time.Duration(rule.RecoverTimeout))
			b.cut.Set(false)
		}, logger)
	}
	return false
}

// error count circuit breaker will cut resource if the error count of resource exceed the threshold of rule.
type errorCountCircuitBreaker struct {
	// status of the breaker
	cut util.AtomicBool
	// the count of request exceed the threshold
	passCount int64
	rule      *errorCountRule
	metric    base.ReadStat
}

func newErrorCountCircuitBreaker(rule *errorCountRule) *errorCountCircuitBreaker {
	resNode := stat.GetResourceNode(rule.Resource)
	var metric base.ReadStat
	// TODO need to optimize, we should to handle the scenario that resNode is nil
	if resNode != nil {
		metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
	}
	return &errorCountCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

func newErrorCountCircuitBreakerWithMetric(rule *errorCountRule, metric base.ReadStat) *errorCountCircuitBreaker {
	return &errorCountCircuitBreaker{
		rule:   rule,
		metric: metric,
	}
}

func (b *errorCountCircuitBreaker) getRule() Rule {
	return b.rule
}

func (b *errorCountCircuitBreaker) TryPass(_ *base.EntryContext) bool {
	if b.cut.Get() {
		return false
	}

	rule := b.rule
	if rule == nil {
		return true
	}

	// TODO need to optimize here.
	// We might create individual stat structures for circuit breakers, rather than use the universal ResourceNode.
	if b.metric == nil {
		resNode := stat.GetResourceNode(rule.Resource)
		if resNode == nil {
			logger.Errorf("Resource(%s)'s stat node is nil.", rule.Resource)
			return true
		}
		b.metric = resNode.GetOrCreateSlidingWindowMetric(rule.SampleCount, rule.IntervalInMs)
		logger.Errorf("Delayed to initialize the metric of errorCountCircuitBreaker.")
	}

	err := b.metric.GetQPS(base.MetricEventError)
	if err < float64(rule.Threshold) {
		return true
	}

	if b.cut.CompareAndSet(false, true) {
		go util.RunWithRecover(func() {
			// recover after RecoverTimeout seconds
			time.Sleep(time.Second * time.Duration(rule.RecoverTimeout))
			b.cut.Set(false)
		}, logger)
	}
	return false
}
