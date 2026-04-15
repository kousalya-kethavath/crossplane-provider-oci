package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/oracle/provider-oci/internal/upgradeaudit"
)

func main() {
	var (
		oldDir  string
		newDir  string
		outFile string
		quiet   bool
		indent  bool
	)

	pflag.StringVar(&oldDir, "old", "", "Path to the baseline CRD directory (or package root)")
	pflag.StringVar(&newDir, "new", "", "Path to the updated CRD directory (or package root)")
	pflag.StringVar(&outFile, "out", "", "Optional path to write the JSON report")
	pflag.BoolVar(&quiet, "quiet", false, "Suppress stdout output (use with --out)")
	pflag.BoolVar(&indent, "indent", true, "Pretty-print JSON output")
	pflag.Parse()

	if err := validateInputs(oldDir, newDir, outFile, quiet); err != nil {
		fail(err)
	}

	report, err := upgradeaudit.GenerateReport(oldDir, newDir)
	if err != nil {
		fail(err)
	}

	data, err := report.Marshal(indent)
	if err != nil {
		fail(fmt.Errorf("marshal report: %w", err))
	}

	if outFile != "" {
		if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
			fail(fmt.Errorf("create output dir: %w", err))
		}
		if err := os.WriteFile(outFile, data, 0o644); err != nil {
			fail(fmt.Errorf("write report: %w", err))
		}
	}

	if !quiet {
		fmt.Println(string(data))
	}
}

func validateInputs(oldDir, newDir, outFile string, quiet bool) error {
	if oldDir == "" || newDir == "" {
		return errors.New("both --old and --new must be provided")
	}
	if quiet && outFile == "" {
		return errors.New("--quiet requires --out to capture output")
	}
	return nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
