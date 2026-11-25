// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: CC0-1.0

package storage

import (
	"time"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
)

// Configure configures individual resources by adding custom
// ResourceConfigurators.
func Configure(p *config.Provider) {
	p.AddResourceConfigurator("google_storage_bucket_object", func(r *config.Resource) {
		// Note(turkenh): We have to modify schema of
		// "customer_encryption", since it is struct marked as sensitive.
		// https://github.com/crossplane/upjet/issues/100#issuecomment-966892273
		r.TerraformResource.Schema["customer_encryption"].Sensitive = false
	})

	p.AddResourceConfigurator("google_storage_bucket", func(r *config.Resource) {
		// Note(turkenh): Terraform provider added retries to bucket resource
		// for eventual consistency: https://github.com/hashicorp/terraform-provider-google/pull/10287
		// However, this causes refresh to keep retrying until timeout if the
		// bucket does not exist indeed. This causes the initial observe call to
		// hang on until timeout. We configure read timeout to a relatively
		// smaller value as a workaround/solution.
		// Related issue: https://github.com/upbound/provider-gcp/issues/12
		r.OperationTimeouts.Read = 1 * time.Minute
		r.Version = "v1beta2"
		r.PreviousVersions = []string{"v1beta1"}
		r.SetCRDStorageVersion("v1beta1")
		r.ControllerReconcileVersion = "v1beta2" //nolint:staticcheck // still handling the deprecated behavior
		r.Conversions = append([]conversion.Conversion{
			conversion.NewIdentityConversionExpandPaths("*", "*", conversion.DefaultPathPrefixes(), "retentionPolicy.retentionPeriod"),
			conversion.NewIdentityConversionExpandPaths("*", "*", conversion.DefaultPathPrefixes(), "retentionPolicy.retentionPeriod"),
		}, retentionPeriodConversions()...)
	})

	p.AddResourceConfigurator("google_storage_bucket_iam_member", func(r *config.Resource) {
		r.References["bucket"] = config.Reference{
			TerraformName: "google_storage_bucket",
		}
	})

	p.AddResourceConfigurator("google_storage_bucket_iam_policy", func(r *config.Resource) {
		r.References["bucket"] = config.Reference{
			TerraformName: "google_storage_bucket",
		}
	})

	p.AddResourceConfigurator("google_storage_bucket_object", func(r *config.Resource) {
		r.TerraformResource.Schema["content"].Sensitive = false
		r.References["bucket"] = config.Reference{
			TerraformName: "google_storage_bucket",
		}
	})

	p.AddResourceConfigurator("google_storage_hmac_key", func(r *config.Resource) {
		r.References["service_account_email"] = config.Reference{
			TerraformName: "google_service_account",
			Extractor:     `github.com/crossplane/upjet/v2/pkg/resource.ExtractParamPath("email",true)`,
		}
	})

	p.AddResourceConfigurator("google_storage_managed_folder_iam_member", func(r *config.Resource) {
		r.References["managed_folder"] = config.Reference{
			TerraformName: "google_storage_managed_folder",
			Extractor:     `github.com/crossplane/upjet/v2/pkg/resource.ExtractParamPath("name",true)`,
		}
		r.References["bucket"] = config.Reference{
			TerraformName: "google_storage_bucket",
		}
	})
}

// retentionPeriodConversions convert retentionPolicy.retentionPeriod between
// the storage version v1beta1, where it is a number, and v1beta2, where it is a
// string, exactly.
//
// The period is a whole number of seconds. FloatToString formats with the
// shortest 'g' precision, which switches to exponent notation at 1e6 or more
// (2592000 becomes "2.592e+06"); the Terraform provider then parses the
// string as an integer, discards the error and sets no retention policy, or
// fails to lock one that is missing. IntToString and StringToInt keep every
// whole number exact.
// See: https://github.com/crossplane-contrib/provider-upjet-gcp/issues/1071
func retentionPeriodConversions() []conversion.Conversion {
	paths := []string{
		"spec.forProvider.retentionPolicy.retentionPeriod",
		"spec.initProvider.retentionPolicy.retentionPeriod",
		"status.atProvider.retentionPolicy.retentionPeriod",
	}
	out := make([]conversion.Conversion, 0, 2*len(paths))
	for _, p := range paths {
		out = append(out,
			conversion.NewFieldTypeConversion("v1beta1", "v1beta2", p, conversion.IntToString),
			conversion.NewFieldTypeConversion("v1beta2", "v1beta1", p, conversion.StringToInt))
	}
	return out
}
