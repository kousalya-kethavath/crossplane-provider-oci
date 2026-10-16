/*
Copyright 2026 Oracle and/or its affiliates.
*/

package v1beta1

import (
	"os"
	"path/filepath"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuralcel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	customvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

func TestProviderConfigCredentialAdmissionRules(t *testing.T) {
	for _, kind := range []string{"providerconfigs", "clusterproviderconfigs"} {
		t.Run(kind, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "package", "crds", "oci.m.upbound.io_"+kind+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var crd apiextensionsv1.CustomResourceDefinition
			if err := yaml.Unmarshal(data, &crd); err != nil {
				t.Fatal(err)
			}
			var props apiextensions.JSONSchemaProps
			if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &props, nil); err != nil {
				t.Fatal(err)
			}
			structural, err := structuralschema.NewStructural(&props)
			if err != nil {
				t.Fatal(err)
			}
			validator, _, err := customvalidation.NewSchemaValidator(&props)
			if err != nil {
				t.Fatal(err)
			}
			cel := structuralcel.NewValidator(structural, true, celconfig.PerCallLimit)
			tests := []struct {
				name, region, source, sa string
				secret                   bool
				valid                    bool
			}{
				{"existing secret", "", "Secret", "", true, true},
				{"existing environment", "", "Environment", "", false, true},
				{"existing filesystem", "", "Filesystem", "", false, true},
				{"existing injected identity schema", "", "InjectedIdentity", "", false, true},
				{"valid service account", "us-ashburn-1", "ServiceAccount", "oci-access", false, kind == "providerconfigs"},
				{"valid dotted account", "us-ashburn-1", "ServiceAccount", "oci.access", false, kind == "providerconfigs"},
				{"missing name", "us-ashburn-1", "ServiceAccount", "", false, false},
				{"invalid name", "us-ashburn-1", "ServiceAccount", "tenant-b/oci-access", false, false},
				{"missing region", "", "ServiceAccount", "oci-access", false, false},
				{"whitespace region", " ", "ServiceAccount", "oci-access", false, false},
				{"URL instead of region", "https://example.com", "ServiceAccount", "oci-access", false, false},
				{"conflicting secret", "us-ashburn-1", "ServiceAccount", "oci-access", true, false},
			}
			if kind == "providerconfigs" {
				tests = append(tests, struct {
					name, region, source, sa string
					secret                   bool
					valid                    bool
				}{"reference with old source", "", "Secret", "oci-access", true, false})
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					creds := map[string]any{"source": tc.source}
					if tc.sa != "" {
						creds["serviceAccountRef"] = map[string]any{"name": tc.sa}
					}
					if tc.secret {
						creds["secretRef"] = map[string]any{"name": "credentials", "namespace": "tenant-a", "key": "credentials"}
					}
					spec := map[string]any{"credentials": creds}
					if tc.region != "" {
						spec["region"] = tc.region
					}
					obj := map[string]any{"apiVersion": crd.Spec.Group + "/v1beta1", "kind": crd.Spec.Names.Kind, "metadata": map[string]any{"name": "test"}, "spec": spec}
					errs := customvalidation.ValidateCustomResource(field.NewPath(""), obj, validator)
					if cel != nil {
						rules, _ := cel.Validate(t.Context(), field.NewPath(""), structural, obj, nil, celconfig.RuntimeCELCostBudget)
						errs = append(errs, rules...)
					}
					if tc.valid != (len(errs) == 0) {
						t.Fatalf("valid=%v errors=%v", tc.valid, errs)
					}
				})
			}
		})
	}
}
