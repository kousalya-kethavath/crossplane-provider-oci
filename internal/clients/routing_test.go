/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubetesting "k8s.io/client-go/testing"

	namespacedv1beta1 "github.com/oracle/provider-oci/apis/namespaced/v1beta1"
)

type routedManaged struct {
	fake.ModernManaged
	tfType string
}

func (m *routedManaged) GetTerraformResourceType() string { return m.tfType }

func testMixedResourceConfiguration() map[string]*ujconfig.Resource {
	provider := ujconfig.NewProvider([]byte(`{"format_version":"0.2","provider_schemas":{"registry.terraform.io/oracle/oci":{"resource_schemas":{
 "oci_sdk":{"block":{"attributes":{"id":{"type":"string","computed":true}}}},
 "oci_cli":{"block":{"attributes":{"id":{"type":"string","computed":true}}}},
 "oci_framework":{"block":{"attributes":{"id":{"type":"string","computed":true}}}}
 }}}}`), "oci", "github.com/oracle/provider-oci", nil,
		ujconfig.WithIncludeList([]string{"^oci_cli$"}),
		ujconfig.WithTerraformPluginSDKIncludeList([]string{"^oci_sdk$"}),
		ujconfig.WithTerraformPluginFrameworkIncludeList([]string{"^oci_framework$"}),
		ujconfig.WithTerraformPluginFrameworkProvider(routingFrameworkProvider{}),
		ujconfig.WithTerraformProvider(&schema.Provider{ResourcesMap: map[string]*schema.Resource{
			"oci_sdk": {Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}}},
		}}))
	return provider.Resources
}

// These schema-only fakes let Upjet set its own private connector-selection
// flags; authentication setup still constructs the real OCI Framework provider.
type routingFrameworkProvider struct{ fwprovider.Provider }

func (routingFrameworkProvider) Metadata(_ context.Context, _ fwprovider.MetadataRequest, resp *fwprovider.MetadataResponse) {
	resp.TypeName = "oci"
}
func (routingFrameworkProvider) Resources(context.Context) []func() fwresource.Resource {
	return []func() fwresource.Resource{func() fwresource.Resource { return routingFrameworkResource{} }}
}

type routingFrameworkResource struct{ fwresource.Resource }

func (routingFrameworkResource) Metadata(_ context.Context, _ fwresource.MetadataRequest, resp *fwresource.MetadataResponse) {
	resp.TypeName = "oci_framework"
}
func (routingFrameworkResource) Schema(_ context.Context, _ fwresource.SchemaRequest, resp *fwresource.SchemaResponse) {
	resp.Schema = fwschema.Schema{Attributes: map[string]fwschema.Attribute{"id": fwschema.StringAttribute{Computed: true}}}
}

func TestServiceAccountSetupUsesResourceRouteWithSharedCLIConfiguration(t *testing.T) {
	// Initialize the SDK's OKE environment, but deny TokenRequest below. No OCI
	// endpoint is contacted, and no developer's local OCI credentials are used.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCI_RESOURCE_PRINCIPAL_VERSION", "2.2")
	t.Setenv("OCI_RESOURCE_PRINCIPAL_REGION", "us-ashburn-1")
	t.Setenv("OCI_KUBERNETES_SERVICE_ACCOUNT_CERT_PATH", caPath)
	t.Setenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")

	s, tokens, kube := serviceAccountFixture(t)
	if err := namespacedv1beta1.SchemeBuilder.AddToScheme(kube.Scheme()); err != nil {
		t.Fatal(err)
	}
	pc := &namespacedv1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "tenant-oci", UID: "pc-a"}, Spec: *serviceAccountConfig("tenant-a").spec}
	if err := kube.Create(t.Context(), pc); err != nil {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	setup := TerraformSetupBuilder(WithServiceAccountCredentials(lifetime, s.reader, s.tokens), WithResourceConfiguration(testMixedResourceConfiguration()), WithTerraformCLIConfiguration("1.4.6", "oracle/oci", "9.0.0"))
	mg := &routedManaged{ModernManaged: fake.ModernManaged{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "resource", UID: types.UID("cnpm2805-" + rand.Text())}, TypedProviderConfigReferencer: fake.TypedProviderConfigReferencer{Ref: &xpv1.ProviderConfigReference{Name: pc.Name, Kind: "ProviderConfig"}}}}
	workspace := filepath.Join(os.TempDir(), string(mg.GetUID()))
	defer os.RemoveAll(workspace)

	mg.tfType = "oci_framework"
	configured, err := setup(t.Context(), kube, mg)
	if err != nil {
		t.Fatal(err)
	}
	if configured.FrameworkProvider == nil || configured.Meta != nil || configured.Scheduler != nil || configured.Requirement.Source != "" || configured.Configuration[credentialKeyOKETokenPath] != nil {
		t.Fatal("Framework route received SDKv2/CLI setup")
	}
	if len(tokens.Actions()) != 0 {
		t.Fatal("Framework setup eagerly requested a token")
	}

	// Deny token acquisition so SDKv2 setup stops before an OCI network request.
	// It must attempt embedded provider configuration, never write a CLI token.
	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) { return true, nil, context.DeadlineExceeded })
	mg.tfType = "oci_sdk"
	configured, err = setup(t.Context(), kube, mg)
	if err == nil || !strings.Contains(err.Error(), "cannot get or init OCI provider") {
		t.Fatalf("SDKv2 route changed to CLI: %v", err)
	}
	if configured.Requirement.Source != "" || configured.Configuration[credentialKeyOKETokenPath] != nil {
		t.Fatal("SDKv2 setup received CLI configuration")
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("SDKv2 setup created a token workspace")
	}
	if len(tokens.Actions()) == 0 {
		t.Fatalf("SDKv2 setup did not use the ServiceAccount supplier: %v", err)
	}

	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) {
		exp := time.Now().Add(time.Hour)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, types.NamespacedName{Namespace: "tenant-a", Name: "oci-access"}, "sa-tenant-a", exp), ExpirationTimestamp: metav1.NewTime(exp)}}, nil
	})
	mg.tfType = "oci_cli"
	configured, err = setup(t.Context(), kube, mg)
	if err != nil {
		t.Fatal(err)
	}
	if configured.Requirement.Source != "oracle/oci" || configured.Version != "1.4.6" || configured.Meta != nil || configured.FrameworkProvider != nil || configured.Scheduler != nil {
		t.Fatalf("invalid CLI route: %#v", configured)
	}
	path, ok := configured.Configuration[credentialKeyOKETokenPath].(string)
	if !ok {
		t.Fatal("CLI route missing token file path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	actions := len(tokens.Actions())
	mg.tfType = "oci_unknown"
	if _, err := setup(t.Context(), kube, mg); err == nil || !strings.Contains(err.Error(), "no configured Terraform execution route") {
		t.Fatalf("unknown resource accepted: %v", err)
	}
	if len(tokens.Actions()) != actions {
		t.Fatal("unknown resource requested a token")
	}
}

func TestFrameworkRoutingDoesNotApplyToOtherResources(t *testing.T) {
	options := setupOptions{isFrameworkResource: func(name string) bool { return name == "oci_framework" }}
	if !options.shouldConfigureFrameworkProvider(&routedManaged{tfType: "oci_framework"}) {
		t.Fatal("framework route rejected")
	}
	for _, name := range []string{"oci_sdk", "oci_cli"} {
		if options.shouldConfigureFrameworkProvider(&routedManaged{tfType: name}) {
			t.Fatalf("framework setup leaked to %s", name)
		}
	}
}

func TestSecretAuthenticationPreservedAcrossResourceRoutes(t *testing.T) {
	_, _, kube := serviceAccountFixture(t)
	if err := namespacedv1beta1.SchemeBuilder.AddToScheme(kube.Scheme()); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := json.Marshal(map[string]string{"auth": "APIKey", "region": "us-ashburn-1", "tenancy_ocid": "ocid1.tenancy.oc1..test", "user_ocid": "ocid1.user.oc1..test", "fingerprint": "test", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))})
	if err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "credentials"}, Data: map[string][]byte{"credentials": credentials}}
	if err := kube.Create(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	pc := &namespacedv1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "tenant-oci", UID: "pc-a"}, Spec: namespacedv1beta1.ProviderConfigSpec{Credentials: namespacedv1beta1.ProviderCredentials{Source: xpv1.CredentialsSourceSecret, CommonCredentialSelectors: xpv1.CommonCredentialSelectors{SecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Namespace: "tenant-a", Name: secret.Name}, Key: "credentials"}}}}}
	if err := kube.Create(t.Context(), pc); err != nil {
		t.Fatal(err)
	}
	setup := TerraformSetupBuilder(WithResourceConfiguration(testMixedResourceConfiguration()), WithTerraformCLIConfiguration("1.4.6", "oracle/oci", "9.0.0"))
	for _, route := range []string{"sdk", "framework", "cli"} {
		t.Run(route, func(t *testing.T) {
			mg := &routedManaged{tfType: "oci_" + route, ModernManaged: fake.ModernManaged{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "resource", UID: types.UID("cnpm2805-" + rand.Text())}, TypedProviderConfigReferencer: fake.TypedProviderConfigReferencer{Ref: &xpv1.ProviderConfigReference{Name: pc.Name, Kind: "ProviderConfig"}}}}
			configured, err := setup(t.Context(), kube, mg)
			if err != nil {
				t.Fatal(err)
			}
			if (configured.Meta != nil) != (route == "sdk") || (configured.FrameworkProvider != nil) != (route == "framework") || (configured.Requirement.Source != "") != (route == "cli") {
				t.Fatalf("wrong setup for %s", route)
			}
			if configured.Configuration[credentialKeyOKETokenPath] != nil {
				t.Fatal("Secret authentication received a ServiceAccount token file")
			}
			if _, err := os.Stat(filepath.Join(os.TempDir(), string(mg.GetUID()))); !os.IsNotExist(err) {
				t.Fatal("Secret authentication created a token workspace")
			}
		})
	}
}
