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

package workload

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"

	// Dirigent
	"github.com/vhive-serverless/loader/pkg/common"
	"github.com/vhive-serverless/loader/pkg/config"
	"github.com/vhive-serverless/loader/pkg/generator"
	"github.com/vhive-serverless/loader/pkg/trace"
)

func LoadTraceFromConfig(path string) []*TraceSpec {
	functions := LoadDirigentTraceFromConfig(path)
	specs := make([]*TraceSpec, 0, len(functions))
	for _, function := range functions {
		specs = append(specs, TranslateDirigentFunction(function))
	}
	return specs
}

// iat is independent per minute, in microseconds
// we convert it to the absolute arrival time, in seconds
func TranslateDirigentFunction(function *common.Function) *TraceSpec {
	rawSpec := function.Specification
	spec := &TraceSpec{
		DurationMinutes: len(rawSpec.PerMinuteCount),
		Invocations:     make([]*InvocationSpec, 0, len(rawSpec.IAT)),
	}
	reqIndex := 0
	for minute, nReqsThisMinute := range rawSpec.PerMinuteCount {
		startOfThisMinute := float64(minute) * 60.
		elaspedInThisMinute := 0.
		for i := 0; i < nReqsThisMinute; i++ {
			elaspedInThisMinute += rawSpec.IAT[reqIndex] / float64(time.Microsecond)
			absArrivalTime := startOfThisMinute + elaspedInThisMinute
			runtimeMilliSec := rawSpec.RuntimeSpecification[reqIndex].Runtime
			spec.Invocations = append(spec.Invocations, &InvocationSpec{
				ArrivalTimeSec:  absArrivalTime,
				RuntimeMilliSec: runtimeMilliSec,
			})
			reqIndex++
		}
	}
	if len(spec.Invocations) != len(rawSpec.IAT) {
		klog.Fatalf("Invocation count mismatch: expected %d, got %d", len(rawSpec.IAT), len(spec.Invocations))
	}
	return spec
}

func LoadDirigentTraceFromConfig(path string) []*common.Function {
	cfg := config.ReadConfigurationFile(path)
	if cfg.Platform != "Dirigent" {
		klog.Fatalf("Invalid loader platform: expected Dirigent, got %s", cfg.Platform)
	}
	if cfg.ExperimentDuration < 1 {
		klog.Fatal("Runtime duration should be longer, at least a minute.")
	}

	durationToParse := determineDurationToParse(cfg.ExperimentDuration, cfg.WarmupDuration)

	// yamlPath is always empty for dirigent
	// https://github.com/vhive-serverless/invitro/blob/0b0d6d7ee59e820a2472a568c89740e0ad157b69/cmd/loader.go#L162
	yamlPath := ""
	// yamlPath := parseYAMLSpecification(cfg)

	traceParser := trace.NewAzureParser(cfg.TracePath, yamlPath, durationToParse)
	functions := traceParser.Parse(cfg.Platform)

	klog.Infof("Traces contain the following %d functions:\n", len(functions))
	for _, function := range functions {
		fmt.Printf("\t%s\n", function.Name)
	}

	iatDistribution, shiftIAT := parseIATDistribution(&cfg)
	traceGranularity := parseTraceGranularity(&cfg)

	if traceGranularity != common.MinuteGranularity {
		klog.Fatal("Expect minute granularity for Azure traces")
	}

	specificationGenerator := generator.NewSpecificationGenerator(cfg.Seed)

	for i, function := range functions {
		spec := specificationGenerator.GenerateInvocationData(
			function,
			iatDistribution,
			shiftIAT,
			traceGranularity,
		)
		if len(spec.IAT) != len(spec.RuntimeSpecification) {
			klog.Fatalf("IAT and runtime spec array length mismatch: expected %d, got %d", len(spec.IAT), len(spec.RuntimeSpecification))
		}
		functions[i].Specification = spec
	}
	return functions
}

func determineDurationToParse(runtimeDuration int, warmupDuration int) int {
	result := 0

	if warmupDuration > 0 {
		result += 1              // profiling
		result += warmupDuration // warmup
	}

	result += runtimeDuration // actual experiment

	return result
}

func parseIATDistribution(cfg *config.LoaderConfiguration) (common.IatDistribution, bool) {
	switch cfg.IATDistribution {
	case "exponential":
		return common.Exponential, false
	case "exponential_shift":
		return common.Exponential, true
	case "uniform":
		return common.Uniform, false
	case "uniform_shift":
		return common.Uniform, true
	case "equidistant":
		return common.Equidistant, false
	default:
		klog.Fatal("Unsupported IAT distribution.")
	}

	return common.Exponential, false
}

func parseTraceGranularity(cfg *config.LoaderConfiguration) common.TraceGranularity {
	switch cfg.Granularity {
	case "minute":
		return common.MinuteGranularity
	case "second":
		return common.SecondGranularity
	default:
		klog.Fatal("Invalid trace granularity parameter.")
	}

	return common.MinuteGranularity
}
