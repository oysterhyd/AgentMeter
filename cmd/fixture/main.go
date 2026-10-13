package main

import (
	"log"
	"os"
	"path/filepath"

	"local.codex.monitor/internal/testfixture"
)

func main() {
	if len(os.Args) != 2 && (len(os.Args) != 3 || os.Args[2] != "--chart-history") {
		log.Fatal("usage: go run ./cmd/fixture NEW_PROFILE_DIRECTORY [--chart-history]")
	}
	dir, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	seed := testfixture.Seed
	if len(os.Args) == 3 {
		seed = testfixture.SeedChartHistory
	}
	if err = seed(dir); err != nil {
		log.Fatal(err)
	}
}
