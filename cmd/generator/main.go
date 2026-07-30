/*
Copyright 2021 Upbound Inc.
*/

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crossplane/upjet/v2/pkg/pipeline"

	"github.com/oracle/provider-oci/config"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "" {
		panic("root directory is required to be given as argument")
	}
	rootDir := os.Args[1]
	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		panic(fmt.Sprintf("cannot calculate the absolute path with %s", rootDir))
	}
	pipeline.Run(config.GetProviderForGeneration(), config.GetProviderNamespacedForGeneration(), absRootDir)
	if err := registerTerraformMetricRecorders(absRootDir); err != nil {
		panic(fmt.Sprintf("cannot register generated Terraform metric recorders: %v", err))
	}
}

var terraformMetricRecorderPattern = regexp.MustCompile(`tjcontroller\.WithTerraformPluginSDKAsyncMetricRecorder\(metrics\.NewMetricRecorder\(([^,\n]+), mgr, o\.PollInterval\)\)`)

func registerTerraformMetricRecorders(rootDir string) error {
	controllerRoot := filepath.Join(rootDir, "internal", "controller")
	return filepath.WalkDir(controllerRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "zz_controller.go" {
			return nil
		}
		return registerTerraformMetricRecorder(path)
	})
}

func registerTerraformMetricRecorder(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	matches := terraformMetricRecorderPattern.FindSubmatch(content)
	if len(matches) == 0 {
		return nil
	}
	if len(terraformMetricRecorderPattern.FindAll(content, -1)) != 1 {
		return fmt.Errorf("%s contains multiple Terraform metric recorder expressions", path)
	}

	const optionsDeclaration = "\topts := []managed.ReconcilerOption{\n"
	if strings.Count(string(content), optionsDeclaration) != 1 {
		return fmt.Errorf("%s does not contain one reconciler options declaration", path)
	}
	registration := fmt.Sprintf(
		"\tmetricRecorder := metrics.NewMetricRecorder(%s, mgr, o.PollInterval)\n"+
			"\tif err := mgr.Add(metricRecorder); err != nil {\n"+
			"\t\treturn errors.Wrap(err, \"cannot register Terraform metric recorder\")\n"+
			"\t}\n",
		matches[1],
	)
	updated := strings.Replace(string(content), optionsDeclaration, registration+optionsDeclaration, 1)
	updated = terraformMetricRecorderPattern.ReplaceAllString(updated, "tjcontroller.WithTerraformPluginSDKAsyncMetricRecorder(metricRecorder)")

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), info.Mode().Perm())
}
