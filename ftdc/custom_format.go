package ftdc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
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
	// panic("this is broken")
	ret := make([]float32, 0, 3*len(mapOrder))
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
				ret = append(ret, float32(rField.Int()))
			case rField.CanFloat():
				ret = append(ret, float32(rField.Float()))
			default:
				// Embedded structs?
				fmt.Println("Bad number type. Type:", rField.Type())
				return nil
			}
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

	fmt.Println("Curr:", curr)
	fmt.Println("Diffs:", diffs)
	matchingBits := make([]byte, numBytes)
	for diffIdx := range diffs {
		// Leading bit is the "schema change" bit. For a "data header", the "schema bit" value is 0.
		// Start "diff bits" at index 1.
		bitIdx := diffIdx + 1
		byteIdx := bitIdx / 8
		bitOffset := bitIdx % 8
		if diffs[diffIdx] > 1e-9 {
			fmt.Println("  Setting bit idx to 1. ByteIdx:", byteIdx, " BitOffset:", bitOffset)
			matchingBits[byteIdx] |= (1 << bitOffset)
		}
	}

	// Write out bits signaling which metrics in the schema changed.
	// for _, bits := range matchingBits {
	//  	fmt.Printf("Bits: %#b\n", bits)
	// }
	fmt.Printf("Writing byte: %b\n", matchingBits[0])
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

func readSchema(reader *bufio.Reader) (*Schema, *bufio.Reader) {
	decoder := json.NewDecoder(reader)
	if !decoder.More() {
		panic("no json")
	}

	var fields []string
	if err := decoder.Decode(&fields); err != nil {
		panic(err)
	}

	// We now have fields, e.g: ["metric1.Foo", "metric1.Bar", "metric2.Foo"]. The `mapOrder` should
	// be ["metric1", "metric2"].
	var mapOrder []string
	metricNameSet := make(map[string]struct{})
	for _, field := range fields {
		metricName := field[:strings.Index(field, ".")]
		if _, exists := metricNameSet[metricName]; !exists {
			mapOrder = append(mapOrder, metricName)
			metricNameSet[metricName] = struct{}{}
		}
	}

	return &Schema{
		fields:   fields,
		mapOrder: mapOrder,
	}, bufio.NewReader(io.MultiReader(decoder.Buffered(), reader))
}

func readDiffBits(reader *bufio.Reader, schema *Schema) []int {
	// 1 bit per field + 1 bit for the "schema bit".
	numBits := len(schema.fields) + 1
	// numBits < 8 => numBytes = 1. numBits < 16 => numBytes = 2, etc...
	numBytes := 1 + ((numBits - 1) / 8)

	diffBytes := make([]byte, numBytes)
	_, err := io.ReadFull(reader, diffBytes)
	if err != nil {
		panic(err)
	}

	fmt.Println("ReadFull. NumBytes:", numBytes)
	for idx, diffByt := range diffBytes {
		if idx == 0 {
			fmt.Printf("  Orig: %b\n", diffByt)
			diffByt = diffByt >> 1
		}
		fmt.Printf("  %b\n", diffByt)
	}
	var ret []int
	for fieldIdx := 0; fieldIdx < len(schema.fields); fieldIdx++ {
		bitIdx := fieldIdx + 1
		diffByteOffset := bitIdx / 8
		bitOffset := bitIdx % 8

		if bitValue := diffBytes[diffByteOffset] & (1 << bitOffset); bitValue == 1 {
			ret = append(ret, fieldIdx)
		}
	}

	return ret
}

func readData(reader *bufio.Reader, schema *Schema, diffedFields []int) (map[string]any, error) {
	return nil, nil
}

func (schema *Schema) MapToNames(fieldIndexes []int) []string {
	var ret []string
	for _, idx := range fieldIndexes {
		ret = append(ret, schema.fields[idx])
	}

	return ret
}

func parse(rawReader io.Reader) ([]map[string]any, error) {
	ret := make([]map[string]any, 0)

	// bufio's Reader allows for peeking and potentially better control over how much data to read
	// from disk at a time.
	reader := bufio.NewReader(rawReader)
	var schema *Schema = nil
	for {
		peek, err := reader.Peek(1)
		fmt.Println("Peeking. Err:", err)
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		fmt.Println("  SchemaBit?", peek[0] == 0x1)
		if peek[0] == 0x1 {
			// Advances to end of json
			_, _ = reader.ReadByte()
			schema, reader = readSchema(reader)
			fmt.Println("Schema:", schema)
			continue
		} else if schema == nil {
			panic("First byte must be the magic one")
		}

		diffedFields := readDiffBits(reader, schema)
		fmt.Println("DiffedFields:", diffedFields, "Mapped:", schema.MapToNames(diffedFields))
		data, err := readData(reader, schema, diffedFields)
		if err != nil {
			return ret, err
		}

		ret = append(ret, data)
	}

	return ret, nil
}
