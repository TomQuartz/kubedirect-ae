package workload

import (
	"fmt"
	"strings"
	"time"

	"golang.design/x/chann"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	INVALID_BACKEND
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
	"INVALID_BACKEND",
}

type Request struct {
	ID                   string
	Target               string
	DurationMilliseconds int
	ClientSendTS         time.Time
	GatewayRecvTS        time.Time
	GatewaySendTS        time.Time
	// Relative to the start of client
	ClientRelTime time.Duration
	// Relative to the start of the selected time window
	TraceRelTime time.Duration
	// Relative to day 0 of the full trace, for Aquatope inference
	TraceAbsStartTime time.Duration
}
type Response struct {
	Source              *Request
	Status              ResponseStatus
	NodeName            string
	FunctionStartTS     time.Time
	GatewayRecvTS       time.Time
	ClientRecvTS        time.Time
	RuntimeMilliseconds int
}

func (r *Response) Summary(horizon time.Time) string {
	latency := func(t time.Time) string {
		if t.IsZero() {
			return "N/A"
		}
		base := r.Source.ClientSendTS
		elapsedMilliseconds := float64(t.Sub(base).Nanoseconds()) / 1e6
		return fmt.Sprintf("+%.3fms", elapsedMilliseconds)
	}
	traceTS := fmt.Sprintf("%.3fs", r.Source.TraceRelTime.Seconds())
	CSendReq := fmt.Sprintf("%.3fs", r.Source.ClientRelTime.Seconds())
	GrecvReq := latency(r.Source.GatewayRecvTS)
	GsendReq := latency(r.Source.GatewaySendTS)
	GrecvRes := latency(r.GatewayRecvTS)
	CRecvRes := latency(r.ClientRecvTS)
	delay := latency(r.GatewayRecvTS.Add(-time.Duration(r.RuntimeMilliseconds) * time.Millisecond))
	return fmt.Sprintf("ID: %v, Status: %v, TS: %v, CSendReq: %v, GRecvReq: %v, GSendReq: %v, GRecvRes: %v, CRecvRes: %v, Delay: %v, Runtime: %v/%v, Node: %v\n",
		r.Source.ID, r.Status, traceTS, CSendReq, GrecvReq, GsendReq, GrecvRes, CRecvRes, delay, r.RuntimeMilliseconds, r.Source.DurationMilliseconds, r.NodeName)
}

type RequestBuffer struct {
	*chann.Chann[*Request]
}
type ResponseBuffer struct {
	*chann.Chann[*Response]
}

type Trace struct {
	ID                   string  `yaml:"id"`
	DurationMilliseconds float64 `yaml:"durationMilliseconds"`
	InvocationsPerSecond []int   `yaml:"invocationsPerSecond"`
	// ArrivalTimeData      string    `yaml:"arrivalTimeData"`
	// ArrivalTimeSeconds   []float64 `yaml:"arrivalTimeSeconds"`
}

type Workload struct {
	Day    int      `yaml:"day"`
	Start  int      `yaml:"start"`
	Length int      `yaml:"length"`
	Traces []*Trace `yaml:"traces"`
}

func (w *Workload) String() string {
	return fmt.Sprintf("Day: %v, Start: %vmin, Length: %vmin, #Traces: %v", w.Day, w.Start, w.Length, len(w.Traces))
}
