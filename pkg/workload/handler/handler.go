/*
 * MIT License
 *
 * Copyright (c) 2023 EASL and the vHive community
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package handler

import (
	"fmt"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// static double SQRTSD (double x) {
//     double r;
//     __asm__ ("sqrtsd %1, %0" : "=x" (r) : "x" (x));
//     return r;
// }
import "C"

const EXEC_UNIT int = 1e2

type FunctionType int

const (
	TraceFunction FunctionType = 0
	SleepFunction FunctionType = 1
)

var hostname string
var funcType FunctionType = TraceFunction

// values copied from Dirigent AE
// https://github.com/vhive-serverless/invitro/blob/0b0d6d7ee59e820a2472a568c89740e0ad157b69/workloads/container/trace_func_go.yaml#L31
var iterationMultiplier int = 102

func takeSqrts() C.double {
	var tmp C.double // Circumvent compiler optimizations
	for i := 0; i < EXEC_UNIT; i++ {
		tmp = C.SQRTSD(C.double(10))
	}
	return tmp
}

func busySpin(runtimeMilliSec uint32) {
	totalIterations := iterationMultiplier * int(runtimeMilliSec)

	for i := 0; i < totalIterations; i++ {
		takeSqrts()
	}
}

func TraceFunctionExecution(start time.Time, requestedMilliSec uint32) (msg string) {
	timeConsumedMilliSec := uint32(time.Since(start).Milliseconds())
	if timeConsumedMilliSec < requestedMilliSec {
		requestedMilliSec -= timeConsumedMilliSec
		if requestedMilliSec > 0 {
			busySpin(requestedMilliSec)
		}

		msg = fmt.Sprintf("OK - %s", hostname)
	}

	return msg
}

func EmptyFunctionExecution(start time.Time, requestedMilliSec uint32) (msg string) {
	timeConsumedMilliSec := uint32(time.Since(start).Milliseconds())
	if timeConsumedMilliSec < requestedMilliSec {
		requestedMilliSec -= timeConsumedMilliSec
		if requestedMilliSec > 0 {
			time.Sleep(time.Duration(requestedMilliSec) * time.Millisecond)
		}

		msg = fmt.Sprintf("OK - EMPTY - %s", hostname)
	}

	return msg
}

func readEnvironmentalVariables() {
	if v, ok := os.LookupEnv("ITERATIONS_MULTIPLIER"); ok {
		if intv, err := strconv.Atoi(v); err == nil {
			iterationMultiplier = intv
		} else {
			log.Warn("Failed to parse ITERATIONS_MULTIPLIER environmental variable, using default value.")
		}
	}

	if v, ok := os.LookupEnv("FUNCTION_TYPE"); ok {
		if v == "trace" {
			funcType = TraceFunction
		} else if v == "sleep" {
			funcType = SleepFunction
		} else {
			log.Warn("Failed to parse FUNCTION_TYPE environmental variable, using default value.")
		}
	}

	log.Infof("ITERATIONS_MULTIPLIER = %d\n", iterationMultiplier)
	log.Infof("FUNCTION_TYPE = %d\n", funcType)

	var err error
	hostname, err = os.Hostname()
	if err != nil {
		log.Warn("Failed to get HOSTNAME environmental variable.")
		hostname = "Unknown host"
	}
}
