package ftdc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"go.viam.com/rdk/logging"
)

type Statser1 struct {
	Metric1 int
	Metric2 int
	Metric3 float32
}

type Statser2 struct {
	Metric1 int
	Metric2 int
	Metric3 float32
}

// TestREPL refers to using the Test as a fast-feedback "REPL"
func TestREPL(t *testing.T) {
	datum := Datum{
		Time: time.Now().Unix(),
		Data: map[string]any{
			"s1": Statser1{0, 1, 1.0},
			"s2": Statser2{2131, 11, 0.0},
		},
		generationId: 1,
	}

	testFile, err := os.Create("./viam-server-custom.ftdc")
	if err != nil {
		panic(err)
	}
	defer func() {
		testFile.Close()
		cmd := exec.Command("ls", "-la", "./viam-server-custom.ftdc")
		stdout, err := cmd.Output()
		if err != nil {
			panic(err)
		}
		fmt.Println(string(stdout))
	}()

	fmt.Println(getSchema(datum.Data))

	schema := &Schema{
		mapOrder: []string{"s1", "s2"},
		fields:   []string{"s1.Metric1", "s1.Metric2", "s1.Metric3", "s2.Metric1", "s2.Metric2", "s2.Metric3"},
	}
	writeSchema(schema, testFile)

	flatten1 := flatten(datum, schema.mapOrder)
	fmt.Println("Flatten:", flatten1)
	writeDatum(nil, flatten1, testFile)

	datum2 := Datum{
		Time: time.Now().Unix(),
		Data: map[string]any{
			"s1": Statser1{0, 1, 1.0},
			"s2": Statser2{0, 11, 1.0},
		},
		generationId: 1,
	}

	flatten2 := flatten(datum2, schema.mapOrder)
	writeDatum(flatten1, flatten2, testFile)

}

func TestCustomFormat(t *testing.T) {
	logger := logging.NewTestLogger(t)
	ftdc := NewWithOutputFormat(logger, "custom")

	for idx := 0; idx < 10; idx++ {
		datumV1 := Datum{
			Time: int64(idx),
			Data: map[string]any{
				"s1": Statser1{0, idx, 1.0},
			},
			generationId: 1,
		}

		ftdc.newDatum(datumV1)
	}

	for idx := 10; idx < 20; idx++ {
		datumV2 := Datum{
			Time: int64(idx),
			Data: map[string]any{
				"s1": Statser1{idx, idx, 1.0},
				"s2": Statser2{0, 1 + (idx / 5), 100.0},
			},
			generationId: 2,
		}

		ftdc.newDatum(datumV2)
	}

	ftdc.currOutputFile.Close()

	ftdcFile, err := os.Open("./viam-server-custom.ftdc")
	if err != nil {
		panic(err)
	}
	parsed := parse(ftdcFile)
	fmt.Println("Parsed:", parsed)
}
