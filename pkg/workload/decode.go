package workload

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"

	"gopkg.in/yaml.v2"
)

func NewWorkloadFromBytes(data []byte) (*Workload, error) {
	w := &Workload{}
	if err := yaml.Unmarshal(data, w); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bytes: %v", err)
	}
	// for _, trace := range w.Traces {
	// 	if len(trace.ArrivalTimeSeconds) != 0 {
	// 		continue
	// 	}
	// 	if trace.ArrivalTimeData != "" {
	// 		arrivalTimes, err := decodeArrivalTimes(trace.ArrivalTimeData)
	// 		if err != nil {
	// 			return nil, fmt.Errorf("failed to decode arrival times for trace %v: %v", trace.ID, err)
	// 		}
	// 		trace.ArrivalTimeSeconds = arrivalTimes
	// 		trace.ArrivalTimeData = ""
	// 	} else {
	// 		return nil, fmt.Errorf("trace %v has no invocations", trace.ID)
	// 	}
	// }
	return w, nil
}

func decodeArrivalTimes(data string) ([]float64, error) {
	compressedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	// decompress
	reader, err := zlib.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	// read to a buffer
	var decompressedData bytes.Buffer
	if _, err := io.Copy(&decompressedData, reader); err != nil {
		return nil, err
	}
	// convert the byte array to a float32 slice
	float32Array := make([]float32, decompressedData.Len()/4)
	if err := binary.Read(&decompressedData, binary.LittleEndian, &float32Array); err != nil {
		return nil, err
	}
	// convert float32 to float64
	float64Array := make([]float64, len(float32Array))
	for i, f := range float32Array {
		float64Array[i] = float64(f)
	}
	return float64Array, nil
}
