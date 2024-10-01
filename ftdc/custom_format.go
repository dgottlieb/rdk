package ftdc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

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

	matchingBits := make([]byte, 1+((numPts-1)/8))
	for diffIdx := range diffs {
		matchingBitsOffset := diffIdx / 8
		bitOffset := diffIdx % 8
		if diffs[diffIdx] > 1e-9 {
			matchingBits[matchingBitsOffset] |= (1 << bitOffset)
		}
	}

	for _, bits := range matchingBits {
		fmt.Printf("Bits: %#b\n", bits)
	}
	output.Write(matchingBits)

	for _, diff := range diffs {
		if diff > 1e-9 {
			binary.Write(output, binary.BigEndian, diff)
		}
	}
}

type Schema struct {
	mapOrder []string
	fields   []string
}

func writeSchema(schema *Schema, output io.Writer) {
	encoder := json.NewEncoder(output)
	encoder.Encode(schema.fields)
}
