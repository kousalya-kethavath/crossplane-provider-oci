/*
Copyright 2022 Upbound Inc.
*/

package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpv2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ProviderConfigKind        = "ProviderConfig"
	ClusterProviderConfigKind = "ClusterProviderConfig"
	// CredentialsSourceServiceAccount requests OKE workload identity credentials
	// for a ServiceAccount in the namespaced ProviderConfig's namespace.
	CredentialsSourceServiceAccount xpv1.CredentialsSource = "ServiceAccount"
)

// A ProviderConfigSpec defines the desired state of a ProviderConfig.
// +kubebuilder:validation:XValidation:rule="self.credentials.source != 'ServiceAccount' || (has(self.region) && size(self.region) > 0)",message="region is required for ServiceAccount credentials"
// +kubebuilder:validation:XValidation:rule="self.credentials.source == 'ServiceAccount' || !has(self.region)",message="region must be configured in the credential data for other credential sources"
type ProviderConfigSpec struct {
	// Region is the OCI API region for ServiceAccount credentials.
	// Other sources configure their region in the existing credential data.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	Region string `json:"region,omitempty"`
	// Credentials required to authenticate to this provider.
	Credentials ProviderCredentials `json:"credentials"`
}

// ProviderCredentials required to authenticate.
// +kubebuilder:validation:XValidation:rule="(self.source == 'ServiceAccount') == has(self.serviceAccountRef)",message="serviceAccountRef is required only for ServiceAccount credentials"
// +kubebuilder:validation:XValidation:rule="self.source != 'ServiceAccount' || (!has(self.secretRef) && !has(self.env) && !has(self.fs))",message="ServiceAccount credentials cannot include secretRef, env or fs"
type ProviderCredentials struct {
	// Source of the provider credentials.
	// +kubebuilder:validation:Enum=Secret;InjectedIdentity;Environment;Filesystem;ServiceAccount
	Source xpv1.CredentialsSource `json:"source"`

	xpv1.CommonCredentialSelectors `json:",inline"`

	// ServiceAccountRef selects an account in this ProviderConfig's namespace.
	// +optional
	ServiceAccountRef *ServiceAccountReference `json:"serviceAccountRef,omitempty"`
}

// ServiceAccountReference selects a namespace-local Kubernetes ServiceAccount.
type ServiceAccountReference struct {
	// Name is the ServiceAccount name. Namespace overrides are not supported.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// ClusterProviderConfigSpec preserves the cluster-scoped authentication API.
type ClusterProviderConfigSpec struct {
	// Credentials required to authenticate to this provider.
	Credentials ClusterProviderCredentials `json:"credentials"`
}

// ClusterProviderCredentials excludes namespace-local ServiceAccount selection.
type ClusterProviderCredentials struct {
	// Source of the provider credentials.
	// +kubebuilder:validation:Enum=Secret;InjectedIdentity;Environment;Filesystem
	Source                         xpv1.CredentialsSource `json:"source"`
	xpv1.CommonCredentialSelectors `json:",inline"`
}

// A ProviderConfigStatus reflects the observed state of a ProviderConfig.
type ProviderConfigStatus struct {
	xpv1.ProviderConfigStatus `json:",inline"`
}

// +kubebuilder:object:root=true

// A ProviderConfig configures a Oracle Cloud Infrastructure (OCI) provider.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,provider,oci}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec   `json:"spec"`
	Status ProviderConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig.
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

// +kubebuilder:object:root=true

// A ClusterProviderConfig configures a Oracle Cloud Infrastructure (OCI) provider.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,oci}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
type ClusterProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterProviderConfigSpec `json:"spec"`
	Status ProviderConfigStatus      `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterProviderConfigList contains a list of ClusterProviderConfig.
type ClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterProviderConfig `json:"items"`
}

// A ProviderConfigUsage indicates that a resource is using a ProviderConfig.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,provider,oci}
// +kubebuilder:storageversion
type ProviderConfigUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	xpv2.TypedProviderConfigUsage `json:",inline"`
}

// GetProviderConfigReference of this ProviderConfigUsage.
func (pc *ProviderConfigUsage) GetProviderConfigReference() xpv1.ProviderConfigReference {
	return pc.ProviderConfigReference
}

// GetResourceReference of this ProviderConfigUsage.
func (pc *ProviderConfigUsage) GetResourceReference() xpv1.TypedReference {
	return pc.ResourceReference
}

// SetProviderConfigReference of this ProviderConfigUsage.
func (pc *ProviderConfigUsage) SetProviderConfigReference(r xpv1.ProviderConfigReference) {
	pc.ProviderConfigReference = r
}

// SetResourceReference of this ProviderConfigUsage.
func (pc *ProviderConfigUsage) SetResourceReference(r xpv1.TypedReference) {
	pc.ResourceReference = r
}

// ProviderConfigGroupKind is the GroupKind for ProviderConfig
var ProviderConfigGroupKind = schema.GroupKind{
	Group: Group,
	Kind:  ProviderConfigKind,
}.String()

// ProviderConfigGroupVersionKind is the GroupVersionKind for ProviderConfig
var ProviderConfigGroupVersionKind = schema.GroupVersionKind{
	Group:   Group,
	Version: Version,
	Kind:    ProviderConfigKind,
}

// ClusterProviderConfigGroupKind is the GroupKind for ClusterProviderConfig
var ClusterProviderConfigGroupKind = schema.GroupKind{
	Group: Group,
	Kind:  ClusterProviderConfigKind,
}.String()

// ClusterProviderConfigGroupVersionKind is the GroupVersionKind for ClusterProviderConfig
var ClusterProviderConfigGroupVersionKind = schema.GroupVersionKind{
	Group:   Group,
	Version: Version,
	Kind:    ClusterProviderConfigKind,
}

// ProviderConfigUsageGroupVersionKind is the GroupVersionKind for ProviderConfigUsage
var ProviderConfigUsageGroupVersionKind = schema.GroupVersionKind{
	Group:   Group,
	Version: Version,
	Kind:    "ProviderConfigUsage",
}

// ProviderConfigUsageListGroupVersionKind is the GroupVersionKind for ProviderConfigUsageList
var ProviderConfigUsageListGroupVersionKind = schema.GroupVersionKind{
	Group:   Group,
	Version: Version,
	Kind:    "ProviderConfigUsageList",
}

// +kubebuilder:object:root=true

// ProviderConfigUsageList contains a list of ProviderConfigUsage.
type ProviderConfigUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfigUsage `json:"items"`
}
