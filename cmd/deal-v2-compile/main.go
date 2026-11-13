// deal-v2-compile compiles frozen local input/output without a provider or database.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	snapshot := flag.String("snapshot", "", "sealed input JSON")
	intermediate := flag.String("intermediate", "", "exact saved model output JSON")
	rules := flag.String("rules", "", "explicit executable rules version")
	flag.Parse()
	if *snapshot == "" || *intermediate == "" || *rules == "" {
		return fmt.Errorf("snapshot, intermediate and rules required")
	}
	c, err := compiler.SelectContract(*rules)
	if err != nil {
		return err
	}
	in, err := os.ReadFile(*snapshot)
	if err != nil {
		return err
	}
	mid, err := os.ReadFile(*intermediate)
	if err != nil {
		return err
	}
	result, err := c.Compile(in, mid)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
