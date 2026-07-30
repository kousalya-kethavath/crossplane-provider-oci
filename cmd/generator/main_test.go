/*
Copyright 2026 Oracle and/or its affiliates.
*/

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterTerraformMetricRecorder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zz_controller.go")
	source := `package generated

func Setup(mgr ctrl.Manager, o tjcontroller.Options) error {
	ac := callbacks()
	opts := []managed.ReconcilerOption{
		managed.WithExternalConnecter(
			tjcontroller.WithTerraformPluginSDKAsyncMetricRecorder(metrics.NewMetricRecorder(v1alpha1.Example_GroupVersionKind, mgr, o.PollInterval)),
		),
	}
	return nil
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registerTerraformMetricRecorder(path); err != nil {
		t.Fatalf("registerTerraformMetricRecorder returned error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	for _, want := range []string{
		"metricRecorder := metrics.NewMetricRecorder(v1alpha1.Example_GroupVersionKind, mgr, o.PollInterval)",
		"if err := mgr.Add(metricRecorder); err != nil",
		"tjcontroller.WithTerraformPluginSDKAsyncMetricRecorder(metricRecorder)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("processed controller does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "WithTerraformPluginSDKAsyncMetricRecorder(metrics.NewMetricRecorder") {
		t.Errorf("processed controller retained inline metric recorder:\n%s", got)
	}
}

func TestRegisterTerraformMetricRecorderIgnoresUnrelatedController(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zz_controller.go")
	const source = "package generated\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registerTerraformMetricRecorder(path); err != nil {
		t.Fatalf("registerTerraformMetricRecorder returned error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != source {
		t.Fatalf("unrelated controller changed to %q", string(content))
	}
}
