/*
 * Copyright (c) 2026 Oracle and/or its affiliates
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kms

import (
	"context"
	"fmt"
	"strings"
)

// GetVaultReplicationID validates the composite ID expected by the Terraform
// provider. An empty external name is valid before the provider creates the
// replication; imported and observe-only resources must use vault_id:region.
func GetVaultReplicationID(_ context.Context, externalName string, _ map[string]any, _ map[string]any) (string, error) {
	if externalName == "" {
		return "", nil
	}
	vaultID, replicaRegion, found := strings.Cut(externalName, ":")
	if !found || vaultID == "" || replicaRegion == "" || strings.Contains(replicaRegion, ":") {
		return "", fmt.Errorf("invalid VaultReplication external name %q: expected {vault_id}:{replica_region}", externalName)
	}
	return externalName, nil
}
