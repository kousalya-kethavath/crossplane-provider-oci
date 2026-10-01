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

package self

import (
	"reflect"
	"testing"
)

func TestSubscriptionFieldNameOverrides(t *testing.T) {
	want := map[string]string{
		"RatesInitParameters":            "SubscriptionDimensionsRatesInitParameters",
		"RatesObservation":               "SubscriptionDimensionsRatesObservation",
		"RatesParameters":                "SubscriptionDimensionsRatesParameters",
		"PricingPlanRatesInitParameters": "RatesInitParameters",
		"PricingPlanRatesObservation":    "RatesObservation",
		"PricingPlanRatesParameters":     "RatesParameters",
	}

	if got := subscriptionFieldNameOverrides(); !reflect.DeepEqual(got, want) {
		t.Fatalf("subscriptionFieldNameOverrides() = %#v, want %#v", got, want)
	}
}
