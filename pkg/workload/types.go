package workload

import (
	"fmt"
	"time"

	"golang.design/x/chann"
)

type ResponseStatus int

const (
	SUCCESS ResponseStatus = iota
	FAIL_OVERFLOW
	FAIL_TIMEOUT
	FAIL_CONNECT
	FAIL_SEND
	FAIL_RECV
	FAIL_UNMARSHALL
	INVALID_TARGET
)

func (rs ResponseStatus) String() string {
	return responseStatusReadable[rs]
}

var responseStatusReadable = []string{
	"SUCCESS",
	"FAIL_OVERFLOW",
	"FAIL_TIMEOUT",
	"FAIL_CONNECT",
	"FAIL_SEND",
	"FAIL_RECV",
	"FAIL_UNMARSHALL",
	"INVALID_TARGET",
}

type Request struct {
	ID               string
	Target           string
	DurationMilliSec int
	ClientSendTS     time.Time
	GatewayRecvTS    time.Time
	GatewaySendTS    time.Time
	// Relative to the start of client
	ClientRelTime time.Duration
	// Relative to the start of the selected time window
	TraceRelTime time.Duration
}

type Response struct {
	Source          *Request
	Status          ResponseStatus
	GatewayRecvTS   time.Time
	ClientRecvTS    time.Time
	RuntimeMicroSec int
}

func (r *Response) Summary() string {
	latency := func(t time.Time) string {
		base := r.Source.ClientSendTS
		elapsedMilliseconds := float64(t.Sub(base).Nanoseconds()) / 1e6
		if elapsedMilliseconds < 0 {
			return "N/A"
		}
		return fmt.Sprintf("+%.3fms", elapsedMilliseconds)
	}
	traceTS := fmt.Sprintf("%.3fs", r.Source.TraceRelTime.Seconds())
	CSendReq := fmt.Sprintf("%.3fs", r.Source.ClientRelTime.Seconds())
	GrecvReq := latency(r.Source.GatewayRecvTS)
	GsendReq := latency(r.Source.GatewaySendTS)
	GrecvRes := latency(r.GatewayRecvTS)
	CRecvRes := latency(r.ClientRecvTS)
	delay := latency(r.GatewayRecvTS.Add(-time.Duration(r.RuntimeMicroSec) * time.Microsecond))
	return fmt.Sprintf("ID: %v, Func: %v, Status: %v, TS: %v, CSendReq: %v, GRecvReq: %v, GSendReq: %v, GRecvRes: %v, CRecvRes: %v, Delay: %v, Runtime: %.3f/%vms\n",
		r.Source.ID, r.Source.Target, r.Status, traceTS, CSendReq, GrecvReq, GsendReq, GrecvRes, CRecvRes, delay, float64(r.RuntimeMicroSec)/1000, r.Source.DurationMilliSec)
}

type RequestBuffer = *chann.Chann[*Request]
type ResponseBuffer = *chann.Chann[*Response]

type InvocationSpec struct {
	ArrivalTimeSec  float64
	RuntimeMilliSec int
}

type TraceSpec struct {
	DurationMinutes int
	Invocations     []*InvocationSpec
}
