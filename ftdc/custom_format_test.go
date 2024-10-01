package ftdc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
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

func TestFormat(t *testing.T) {
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
