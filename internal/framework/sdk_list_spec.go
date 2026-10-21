// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package framework

import (
	"context"
	"iter"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/logging"
	inttypes "github.com/hashicorp/terraform-provider-aws/internal/types"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// RegionQuery is the list configuration for a resource that takes no arguments
// of its own. A regional list resource still accepts region, so its query
// cannot be empty. Globally scoped resources use struct{}.
type RegionQuery struct {
	WithRegionModel
}

// SDKListSpec declares a list resource over an SDKv2 resource whose paginated
// list call returns whole objects.
//
// Query is the resource's list configuration model, decoded from the
// configuration before listing starts. Resources that declare no list
// configuration use struct{}.
type SDKListSpec[Item, Query any] struct {
	// ConfigSchema declares the list configuration that Query decodes from.
	// Leave it nil for resources that take no list configuration.
	ConfigSchema func(context.Context) listschema.Schema

	// Items turns the query into the service's page iterator. Required.
	//
	// Diagnostics are reported before listing starts, so building the AWS
	// input belongs here rather than inside the iterator, where a failure
	// could only be reported as a per-item error.
	Items func(context.Context, *conns.AWSClient, Query) (iter.Seq2[Item, error], diag.Diagnostics)

	// ID extracts the Terraform resource ID. Required.
	ID func(Item) string

	// Flatten binds the resource's flatten function. Required. The signature
	// matches what skaff generates, so a scaffolded flatten binds unchanged.
	Flatten func(context.Context, *conns.AWSClient, *Item, *schema.ResourceData) error

	// DisplayName labels the result in Terraform's output. Defaults to the ID.
	DisplayName func(Item) string
}

type sdkListResourceFromSpec[Item, Query any] struct {
	ListResourceWithSDKv2Resource
	spec SDKListSpec[Item, Query]
}

// NewSDKListResource builds a list resource from an SDKv2 schema and a spec.
func NewSDKListResource[Item, Query any](res *schema.Resource, spec SDKListSpec[Item, Query]) inttypes.ListResourceForSDK {
	l := &sdkListResourceFromSpec[Item, Query]{spec: spec}
	l.SetResourceSchema(res)

	return l
}

func (l *sdkListResourceFromSpec[Item, Query]) ListResourceConfigSchema(ctx context.Context, _ list.ListResourceSchemaRequest, response *list.ListResourceSchemaResponse) {
	if l.spec.ConfigSchema == nil {
		response.Schema = listschema.Schema{
			Attributes: map[string]listschema.Attribute{},
			Blocks:     map[string]listschema.Block{},
		}

		return
	}

	response.Schema = l.spec.ConfigSchema(ctx)
}

func (l *sdkListResourceFromSpec[Item, Query]) List(ctx context.Context, request list.ListRequest, stream *list.ListResultsStream) {
	awsClient := l.Meta()

	var query Query
	if request.Config.Raw.IsKnown() && !request.Config.Raw.IsNull() {
		if diags := request.Config.Get(ctx, &query); diags.HasError() {
			stream.Results = list.ListResultsStreamDiagnostics(diags)

			return
		}
	}

	items, diags := l.spec.Items(ctx, awsClient, query)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)

		return
	}

	stream.Results = func(yield func(list.ListResult) bool) {
		for item, err := range items {
			if err != nil {
				yield(fwdiag.NewListResultErrorDiagnostic(err))

				return
			}

			id := l.spec.ID(item)
			ctx := tflog.SetField(ctx, logging.ResourceAttributeKey(names.AttrID), id)

			result := request.NewListResult(ctx)
			rd := l.ResourceData()
			rd.SetId(id)

			// One object that cannot be flattened does not fail the list.
			if err := l.spec.Flatten(ctx, awsClient, &item, rd); err != nil {
				tflog.Error(ctx, "listing: skipping item", map[string]any{
					names.AttrID: id,
					"error":      err.Error(),
				})

				continue
			}

			// Logically deleted: flatten cleared the ID.
			if rd.Id() == "" {
				tflog.Debug(ctx, "listing: skipping item whose ID was cleared", map[string]any{
					names.AttrID: id,
				})

				continue
			}

			result.DisplayName = id
			if l.spec.DisplayName != nil {
				result.DisplayName = l.spec.DisplayName(item)
			}

			l.SetResult(ctx, awsClient, request.IncludeResource, rd, &result)
			if result.Diagnostics.HasError() {
				yield(result)

				return
			}

			if !yield(result) {
				return
			}
		}
	}
}
