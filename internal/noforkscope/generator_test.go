package noforkscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareAndGenerateProvider(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "internal/provider/provider.go", `package provider
import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	core "github.com/oracle/terraform-provider-oci/internal/service/core"
	loadbalancer "github.com/oracle/terraform-provider-oci/internal/service/load_balancer"
	"github.com/oracle/terraform-provider-oci/internal/globalvar"
	tfresource "github.com/oracle/terraform-provider-oci/internal/tfresource"
)
func DataSourcesMap() map[string]*schema.Resource {
	tfresource.RegisterDatasource("alias", core.CoreDataSource())
	_ = loadbalancer.LoadBalancerDataSource()
	return globalvar.OciDatasources
}
func ResourcesMap() map[string]*schema.Resource {
	tfresource.RegisterResource("alias", core.CoreResource())
	return globalvar.OciResources
}
func SchemaMap() map[string]*schema.Schema { return nil }
func ProviderConfig(*schema.ResourceData) (any, error) { return nil, nil }
func Provider() *schema.Provider { return nil }
func New() any { return nil }
`)
	writeTestFile(t, root, "internal/provider/register_resource.go", `package provider
func init() {}
`)
	writeTestFile(t, root, "internal/client/foo_clients.go", `package client
func init() { RegisterOracleClient("foo", nil) }
func (m *OracleClients) FooClient() any { return nil }
`)
	writeTestFile(t, root, "internal/client/provider_clients.go", `package client
type OracleClient struct{}
type OracleClientRegistrations struct { RegisteredClients map[string]*OracleClient }
var OracleClientRegistrationsVar *OracleClientRegistrations
type OracleClients struct{}
func RegisterOracleClient(string, *OracleClient) {}
`)
	writeTestFile(t, root, "internal/service/foo/register_resource.go", `package foo
import "github.com/oracle/terraform-provider-oci/internal/tfresource"
func RegisterResource() { tfresource.RegisterResource("oci_foo_bar", FooBarResource()) }
func FooBarResource() any { return nil }
`)
	writeTestFile(t, root, "internal/service/foo/foo_resource.go", `package foo
func useClient(m any) { _ = m.(*client.OracleClients).FooClient() }
`)
	writeTestFile(t, root, "internal/service/foo/foo_export.go", `package foo
func init() {}
`)
	writeTestFile(t, root, "oci/provider.go", "package oci\n")

	if err := PrepareProvider(root); err != nil {
		t.Fatalf("PrepareProvider() error = %v", err)
	}
	report, err := Generate(root, filepath.Join(root, "scopes.json"), map[string][]string{
		"foo": {"oci_foo_bar"},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(report) != 1 || len(report[0].ClientRegistrars) != 1 || report[0].ClientRegistrars[0] != "RegisterFooClients" {
		t.Fatalf("generated report = %#v", report)
	}

	assertTestFileContains(t, root, "internal/client/foo_clients.go", "func RegisterFooClients()")
	assertTestFileContains(t, root, "internal/client/nofork_client_registrations_full.go", "RegisterAllClients()")
	assertTestFileContains(t, root, "internal/provider/register_resource.go", "//go:build !nofork_scoped")
	assertTestFileContains(t, root, "internal/service/foo/foo_export.go", "//go:build !nofork_scoped")
	assertTestFileContains(t, root, "oci/provider_scoped_foo.go", "client.RegisterFooClients")
	assertTestFileContains(t, root, "oci/provider_scoped_foo.go", `"oci_foo_bar": service0.FooBarResource()`)
	assertTestFileContains(t, root, "oci/provider.go", "//go:build !nofork_scoped")
}

func TestValidBuildTagComponent(t *testing.T) {
	for _, service := range []string{"healthchecks", "ai_vision", "service123"} {
		if !validBuildTagComponent(service) {
			t.Fatalf("validBuildTagComponent(%q) = false", service)
		}
	}
	for _, service := range []string{"", "network-firewall", "Networking", "service.tag"} {
		if validBuildTagComponent(service) {
			t.Fatalf("validBuildTagComponent(%q) = true", service)
		}
	}
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertTestFileContains(t *testing.T, root, relative, expected string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s does not contain %q\n%s", relative, expected, content)
	}
}
