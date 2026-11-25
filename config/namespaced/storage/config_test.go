// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: CC0-1.0

package storage

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/google/go-cmp/cmp"
)

// convertRetention runs every retention period conversion from one version
// of a Bucket to another and returns the target's period.
func convertRetention(t *testing.T, from, to string, period any) any {
	t.Helper()
	bucket := func(version string, period any) *fieldpath.Paved {
		obj := map[string]any{
			"apiVersion": "storage.gcp.m.upbound.io/" + version,
			"kind":       "Bucket",
			"spec":       map[string]any{"forProvider": map[string]any{"retentionPolicy": map[string]any{}}},
		}
		if period != nil {
			obj["spec"].(map[string]any)["forProvider"].(map[string]any)["retentionPolicy"].(map[string]any)["retentionPeriod"] = period
		}
		return fieldpath.Pave(obj)
	}
	src, dst := bucket(from, period), bucket(to, nil)
	for _, c := range retentionPeriodConversions() {
		if _, err := c.(conversion.PavedConversion).ConvertPaved(src, dst); err != nil {
			t.Fatalf("ConvertPaved(%s -> %s): %v", from, to, err)
		}
	}
	got, err := dst.GetValue("spec.forProvider.retentionPolicy.retentionPeriod")
	if err != nil {
		t.Fatalf("no converted retention period: %v", err)
	}
	return got
}

func TestRetentionPeriodConversions(t *testing.T) {
	cases := map[string]struct {
		reason   string
		from, to string
		period   any
		want     any
	}{
		"FourHundredDaysFromStorage": {
			reason: "400 days in seconds must not be written in exponent form",
			from:   "v1beta1", to: "v1beta2",
			period: float64(34560000),
			want:   "34560000",
		},
		"Int64FromStorage": {
			reason: "an int64 period from the API server converts exactly",
			from:   "v1beta1", to: "v1beta2",
			period: int64(315360000),
			want:   "315360000",
		},
		"FractionFromStorage": {
			reason: "a fractional value, which the API admits, converts without error or exponent",
			from:   "v1beta1", to: "v1beta2",
			period: 0.552866514018944,
			want:   "0.552866514018944",
		},
		"ToStorage": {
			reason: "the string form converts back to the same whole number",
			from:   "v1beta2", to: "v1beta1",
			period: "34560000",
			want:   int64(34560000),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := convertRetention(t, tc.from, tc.to, tc.period)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("%s\n-want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
