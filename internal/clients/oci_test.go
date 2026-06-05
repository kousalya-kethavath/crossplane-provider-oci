package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerraformProviderConfigIncludesWorkloadIdentityFederation(t *testing.T) {
	creds := map[string]string{
		credentialKeyTenancyOCID:                     "ocid1.tenancy.oc1..example",
		credentialKeyUserOCID:                        "ocid1.user.oc1..example",
		credentialKeyPrivateKey:                      "private-key",
		credentialKeyPrivateKeyPath:                  "/keys/oci.pem",
		credentialKeyFingerprint:                     "fingerprint",
		credentialKeyRegion:                          "us-ashburn-1",
		credentialKeyAuth:                            "WorkloadIdentityFederation",
		credentialKeyConfigFileProfile:               "DEFAULT",
		credentialKeyWorkloadIdentityTokenPath:       "/var/run/secrets/tokens/oci",
		credentialKeyTokenExchangeDomainURL:          "https://idcs.example.com",
		credentialKeyTokenExchangeAuth:               "InstancePrincipal",
		credentialKeyTokenExchangeClientID:           "client-id",
		credentialKeyTokenExchangeClientSecret:       "client-secret",
		credentialKeyTokenExchangeRequestedTokenType: "urn:oci:token-type:oci-rpst",
		credentialKeyTokenExchangeSubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
		credentialKeyTokenExchangeResourceType:       "resource-type",
		credentialKeyTokenExchangeRPSTExpiration:     "3600",
		credentialKeyTokenExchangePublicKey:          "public-key",
	}

	cfg := terraformProviderConfig(creds)

	for key, want := range creds {
		if got := cfg[key]; got != want {
			t.Fatalf("cfg[%q] = %v, want %q", key, got, want)
		}
	}
}

func TestProviderPatchContainsWorkloadIdentityFederationHook(t *testing.T) {
	patchPath := filepath.Join("..", "..", "hack", "oci-provider-workload-identity.patch")
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("cannot read patch: %v", err)
	}
	patchText := string(patch)

	wantMarkers := []string{
		`AuthWorkloadIdentityFederation        = "WorkloadIdentityFederation"`,
		"TokenExchangeConfigurationProviderFromIssuer",
		"fileTokenIssuer",
		"workload_identity_token_path",
		"token_exchange_domain_url",
		"token_exchange_auth",
		"token_exchange_client_secret",
		"InstancePrincipalConfigurationProvider",
		"InstancePrincipalProvider",
	}
	for _, marker := range wantMarkers {
		if !strings.Contains(patchText, marker) {
			t.Fatalf("patch does not contain %q", marker)
		}
	}
}
