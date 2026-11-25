// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: CC0-1.0

package storage

import (
	"strconv"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
		r.Version = "v1beta3"
		r.PreviousVersions = []string{"v1beta2"}
		r.SetCRDStorageVersion("v1beta2")
		r.ControllerReconcileVersion = "v1beta3" //nolint:staticcheck // still handling the deprecated behavior
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
// the storage version v1beta2, where it is a number, and v1beta3, where it is a
// string, without exponent notation.
//
// upjet's FloatToString formats with the shortest 'g' precision, which
// switches to exponent notation at 1e6 or more (2592000 becomes "2.592e+06");
// terraform-provider-google then parses the string as an integer, discards the
// error and sets no retention policy. It also refuses an int64 value from the
// API server. plainNumberToString formats any number exactly, as upjet's own
// fix (crossplane/upjet#781) does; the reverse direction is unchanged.
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
			&plainNumberToString{sourceVersion: "v1beta2", targetVersion: "v1beta3", fieldPath: p},
			conversion.NewFieldTypeConversion("v1beta3", "v1beta2", p, conversion.StringToFloat))
	}
	return out
}

// plainNumberToString converts a numeric field to its string form without
// exponent notation.
type plainNumberToString struct {
	sourceVersion, targetVersion, fieldPath string
}

// Applicable reports whether the conversion is from sourceVersion to
// targetVersion.
func (c *plainNumberToString) Applicable(src, dst runtime.Object) bool {
	return src.GetObjectKind().GroupVersionKind().Version == c.sourceVersion &&
		dst.GetObjectKind().GroupVersionKind().Version == c.targetVersion
}

// ConvertPaved sets the target field to the source number as a string.
func (c *plainNumberToString) ConvertPaved(src, target *fieldpath.Paved) (bool, error) {
	if !c.Applicable(&unstructured.Unstructured{Object: src.UnstructuredContent()},
		&unstructured.Unstructured{Object: target.UnstructuredContent()}) {
		return false, nil
	}
	v, err := src.GetValue(c.fieldPath)
	if fieldpath.IsNotFound(err) || v == nil {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "cannot get %q", c.fieldPath)
	}
	var s string
	switch n := v.(type) {
	case float64:
		s = strconv.FormatFloat(n, 'f', -1, 64)
	case int64:
		s = strconv.FormatInt(n, 10)
	default:
		return false, errors.Errorf("%q: expected a number, got %T", c.fieldPath, v)
	}
	return true, errors.Wrapf(target.SetValue(c.fieldPath, s), "cannot set %q", c.fieldPath)
}
