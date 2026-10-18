package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/crossplane/upjet/v2/pkg/pipeline/templates"

	"github.com/oracle/provider-oci/hack"
)

func TestGenerateServiceControllerSetupPackages(t *testing.T) {
	root := t.TempDir()
	content := []byte("package controller\n")
	for _, scope := range []string{"cluster", "namespaced"} {
		controllerRoot := filepath.Join(root, "internal", "controller", scope)
		if err := os.MkdirAll(filepath.Join(controllerRoot, "identity"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(controllerRoot, "zz_identity_setup.go"), content, 0o644); err != nil {
			t.Fatal(err)
		}
		// Config intentionally has no service-specific target. Monolith's
		// target is created by the generator because it aggregates all services.
		for _, service := range []string{"config", "monolith"} {
			if err := os.WriteFile(filepath.Join(controllerRoot, "zz_"+service+"_setup.go"), content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := generateServiceControllerSetupPackages(root); err != nil {
		t.Fatalf("generateServiceControllerSetupPackages() error = %v", err)
	}
	for _, scope := range []string{"cluster", "namespaced"} {
		for _, service := range []string{"identity", "monolith"} {
			got, err := os.ReadFile(filepath.Join(root, "internal", "controller", scope, service, "zz_setup.go"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(content) {
				t.Fatalf("generated %s/%s setup = %q, want %q", scope, service, got, content)
			}
		}
	}
}

func TestGenerateServiceControllerSetupPackagesRejectsMissingServiceDirectory(t *testing.T) {
	root := t.TempDir()
	for _, scope := range []string{"cluster", "namespaced"} {
		controllerRoot := filepath.Join(root, "internal", "controller", scope)
		if err := os.MkdirAll(controllerRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(controllerRoot, "zz_identity_setup.go"), []byte("package controller\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := generateServiceControllerSetupPackages(root); err == nil {
		t.Fatal("generateServiceControllerSetupPackages() error = nil, want missing service directory error")
	}
}

func TestAPIPackageForGroup(t *testing.T) {
	tests := map[string]struct {
		group string
		want  apiPackage
	}{
		"cluster": {
			group: "identity.oci.upbound.io",
			want:  apiPackage{scope: "cluster", service: "identity", version: "v1alpha1"},
		},
		"namespaced": {
			group: "identity.oci.m.upbound.io",
			want:  apiPackage{scope: "namespaced", service: "identity", version: "v1alpha1"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := apiPackageForGroup(tt.group, "v1alpha1")
			if err != nil {
				t.Fatalf("apiPackageForGroup() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("apiPackageForGroup() = %#v, want %#v", got, tt.want)
			}
		})
	}
	if _, err := apiPackageForGroup("example.com", "v1alpha1"); err == nil {
		t.Fatal("apiPackageForGroup() error = nil, want unsupported group error")
	}
}

func TestWriteServiceAPIScheme(t *testing.T) {
	root := t.TempDir()
	packages := map[apiPackage]struct{}{
		{scope: "cluster", service: "identity", version: "v1alpha1"}: {},
		{scope: "namespaced", version: "v1beta1"}:                    {},
	}
	for pkg := range packages {
		dir := filepath.Join(root, "apis", pkg.scope)
		if pkg.service != "" {
			dir = filepath.Join(dir, pkg.service)
		}
		if err := os.MkdirAll(filepath.Join(dir, pkg.version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := writeServiceAPIScheme(root, "identity", packages); err != nil {
		t.Fatalf("writeServiceAPIScheme() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "internal", "apis", "runtime", "identity", "zz_scheme.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"apis/cluster/identity/v1alpha1",
		"apis/namespaced/v1beta1",
		"SchemeBuilder.AddToScheme",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("generated scheme does not contain %q", want)
		}
	}
}

func TestControllerTemplatePreservesPerResourceConnectorsAndMetrics(t *testing.T) {
	source := controllerTemplateWithCLIWorkspaceFinalizer(controllerTemplateWithManagedMetricRecorder(templates.ControllerTemplate))
	for _, async := range []bool{false, true} {
		for _, route := range []string{"sdk", "framework", "cli"} {
			t.Run(fmt.Sprintf("%s/async=%t", route, async), func(t *testing.T) {
				vars := map[string]any{"Header": "", "GenStatement": "", "Imports": "", "Package": "resource", "CRD": map[string]string{"Kind": "Example"}, "TypePackageAlias": "v1alpha1.", "UseAsync": async, "UseTerraformPluginSDKClient": route == "sdk", "UseTerraformPluginFrameworkClient": route == "framework", "FeaturesPackageAlias": "features.", "ResourceType": "oci_example"}
				tmpl, err := template.New("controller").Parse(source)
				if err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				if err := tmpl.Execute(&out, vars); err != nil {
					t.Fatal(err)
				}
				got := out.String()
				if _, err := parser.ParseFile(token.NewFileSet(), "zz_controller.go", got, parser.AllErrors); err != nil {
					t.Fatal(err)
				}
				wantConnector := "tjcontroller.NewConnector("
				if route == "sdk" {
					wantConnector = "tjcontroller.NewTerraformPluginSDK"
					if async {
						wantConnector += "Async"
					}
					wantConnector += "Connector("
				}
				if route == "framework" {
					wantConnector = "tjcontroller.NewTerraformPluginFramework"
					if async {
						wantConnector += "Async"
					}
					wantConnector += "Connector("
				}
				if !strings.Contains(got, wantConnector) {
					t.Fatalf("missing %s", wantConnector)
				}
				if route == "cli" {
					if strings.Contains(got, "mgr.Add(metricRecorder)") || strings.Contains(got, "NewOperationTrackerFinalizer") || strings.Contains(got, "WithStatusUpdates(false)") {
						t.Fatal("CLI controller received in-process behavior")
					}
					if !strings.Contains(got, "runtimeconnector.NewCLIWorkspaceFinalizer") {
						t.Fatal("missing CLI token cleanup")
					}
				} else {
					for _, want := range []string{"mgr.Add(metricRecorder)", "NewOperationTrackerFinalizer", "MetricRecorder(metricRecorder)"} {
						if !strings.Contains(got, want) {
							t.Fatalf("missing %s", want)
						}
					}
					if async && !strings.Contains(got, "WithStatusUpdates(false)") {
						t.Fatal("in-process callbacks changed")
					}
					if strings.Contains(got, "runtimeconnector") || strings.Contains(got, "o.WorkspaceStore") {
						t.Fatal("in-process connector depends on CLI workspace presence")
					}
				}
			})
		}
	}
}

func TestMainTemplateHasNoGlobalExecutionOverride(t *testing.T) {
	if strings.Contains(hack.MainTemplate, `Flag("terraform-cli"`) || strings.Contains(hack.MainTemplate, "TERRAFORM_CLI") {
		t.Fatal("global CLI override remains")
	}
	for _, want := range []string{"WithResourceConfiguration(provider.Resources)", "if config.HasCLIResources(provider)", "WithTerraformCLIConfiguration"} {
		if !strings.Contains(hack.MainTemplate, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
