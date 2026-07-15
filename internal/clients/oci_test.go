package clients

import "testing"

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
		credentialKeyTokenExchangeAuth:               "OAuthClientCredentials",
		credentialKeyTokenExchangeClientID:           "client-id",
		credentialKeyTokenExchangeClientSecret:       "client-secret",
		credentialKeyTokenExchangeRequestedTokenType: "urn:oci:token-type:oci-rpst",
		credentialKeyTokenExchangeSubjectTokenType:   "jwt",
		credentialKeyTokenExchangeResourceType:       "k8sworkload",
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
