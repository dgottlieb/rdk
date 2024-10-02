package ftdc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

type Schema struct {
	mapOrder []string
	fields   []string
}

func getSchema(data map[string]any) *Schema {
	var mapOrder []string
	for key, _ := range data {
		mapOrder = append(mapOrder, key)
	}

	var fields []string
	for _, key := range mapOrder {
		stats := data[key]
		rType := reflect.TypeOf(stats)
		for memberIdx := 0; memberIdx < rType.NumField(); memberIdx++ {
			fields = append(fields, fmt.Sprintf("%v.%v", key, rType.Field(memberIdx).Name))
		}
	}

	return &Schema{
		mapOrder: mapOrder,
		fields:   fields,
	}
}

func flatten(datum Datum, mapOrder []string) []float32 {
	ret := make([]float32, 3*len(mapOrder))
	retIdx := 0
	for _, key := range mapOrder {
		stats, exists := datum.Data[key]
		if !exists {
			fmt.Println("Missing data. Need to know how much to skip in the output float32")
			return nil
		}

		rVal := reflect.ValueOf(stats)
		for memberIdx := 0; memberIdx < rVal.NumField(); memberIdx++ {
			rField := rVal.Field(memberIdx)
			switch {
			case rField.CanInt():
				ret[retIdx] = float32(rField.Int())
			case rField.CanFloat():
				ret[retIdx] = float32(rField.Float())
			default:
				// Embedded structs?
				fmt.Println("Bad number type. Type:", rField.Type())
				return nil
			}
			retIdx++
		}
	}

	return ret
}

func writeDatum(prev, curr []float32, output io.Writer) {
	numPts := len(curr)
	if numPts == 0 {
		panic("No points?")
	}

	if len(prev) != 0 && numPts != len(prev) {
		panic(fmt.Sprintf("Bad input sizes. Prev: %v Curr: %v", len(prev), len(curr)))
	}

	diffs := make([]float32, numPts)
	if len(prev) == 0 {
		for idx := range curr {
			diffs[idx] = curr[idx]
		}
	} else {
		for idx := range curr {
			diffs[idx] = curr[idx] - prev[idx]
		}
	}

	// One bit per datapoint. And one leading bit for the "schema change" bit.
	numBits := numPts + 1
	// numBits < 8 => numBytes = 1. numBits < 16 => numBytes = 2, etc...
	numBytes := 1 + ((numBits - 1) / 8)

	matchingBits := make([]byte, numBytes)
	for diffIdx := range diffs {
		// Leading bit is the "schema change" bit. For a "data header", the "schema bit" value is 0.
		// Start "diff bits" at index 1.
		bitIdx := diffIdx + 1
		byteIdx := bitIdx / 8
		bitOffset := bitIdx % 8
		if diffs[diffIdx] > 1e-9 {
			matchingBits[byteIdx] |= (1 << bitOffset)
		}
	}

	// Write out bits signaling which metrics in the schema changed.
	for _, bits := range matchingBits {
		fmt.Printf("Bits: %#b\n", bits)
	}
	output.Write(matchingBits)

	// Write out values for metrics that changed across reading.
	for _, diff := range diffs {
		if diff > 1e-9 {
			binary.Write(output, binary.BigEndian, diff)
		}
	}
}

func writeSchema(schema *Schema, output io.Writer) {
	// New schema byte
	output.Write([]byte{0x1})
	encoder := json.NewEncoder(output)
	encoder.Encode(schema.fields)
}
