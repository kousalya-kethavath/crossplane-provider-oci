/*
Copyright 2022 Upbound Inc.
*/

package apis

import (
	"k8s.io/apimachinery/pkg/runtime"

	clusterapis "github.com/oracle/provider-oci/apis/cluster"
	namespacedapis "github.com/oracle/provider-oci/apis/namespaced"
)

func init() {
	AddToSchemes = append(AddToSchemes, clusterapis.AddToSchemes...)
	AddToSchemes = append(AddToSchemes, namespacedapis.AddToSchemes...)
}

// AddToSchemes may be used to add all resources defined in the project to a Scheme.
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all resources to the Scheme.
func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
