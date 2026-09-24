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

import "testing"

func TestGetVaultReplicationID(t *testing.T) {
	tests := map[string]struct {
		externalName string
		want         string
		wantErr      bool
	}{
		"empty before create": {
			want: "",
		},
		"valid import ID": {
			externalName: "ocid1.vault.oc1.iad.example:us-phoenix-1",
			want:         "ocid1.vault.oc1.iad.example:us-phoenix-1",
		},
		"missing region": {
			externalName: "ocid1.vault.oc1.iad.example",
			wantErr:      true,
		},
		"empty vault ID": {
			externalName: ":us-phoenix-1",
			wantErr:      true,
		},
		"additional separator": {
			externalName: "ocid1.vault.oc1.iad.example:us-phoenix-1:extra",
			wantErr:      true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := GetVaultReplicationID(t.Context(), tt.externalName, nil, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetVaultReplicationID(%q) expected an error", tt.externalName)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetVaultReplicationID(%q) unexpected error: %v", tt.externalName, err)
			}
			if got != tt.want {
				t.Fatalf("GetVaultReplicationID(%q) = %q, want %q", tt.externalName, got, tt.want)
			}
		})
	}
}
