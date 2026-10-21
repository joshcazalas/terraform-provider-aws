// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package framework

import (
	"context"
	"iter"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/logging"
	inttypes "github.com/hashicorp/terraform-provider-aws/internal/types"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// SDKListSpec declares a list resource over an SDKv2 resource whose paginated
// list call returns whole objects.
type SDKListSpec[Item any] struct {
	// Items binds the service's page iterator. Required.
	Items func(context.Context, *conns.AWSClient) iter.Seq2[Item, error]

	// ID extracts the Terraform resource ID. Required.
	ID func(Item) string

	// Flatten binds the resource's flatten function. Required. The signature
	// matches what skaff generates, so a scaffolded flatten binds unchanged.
	Flatten func(context.Context, *conns.AWSClient, *Item, *schema.ResourceData) error

	// DisplayName labels the result in Terraform's output. Defaults to the ID.
	DisplayName func(Item) string
}

type sdkListResourceFromSpec[Item any] struct {
	ListResourceWithSDKv2Resource
	spec SDKListSpec[Item]
}

// NewSDKListResource builds a list resource from an SDKv2 schema and a spec.
func NewSDKListResource[Item any](res *schema.Resource, spec SDKListSpec[Item]) inttypes.ListResourceForSDK {
	l := &sdkListResourceFromSpec[Item]{spec: spec}
	l.SetResourceSchema(res)

	return l
}

func (l *sdkListResourceFromSpec[Item]) List(ctx context.Context, request list.ListRequest, stream *list.ListResultsStream) {
	awsClient := l.Meta()

	stream.Results = func(yield func(list.ListResult) bool) {
		for item, err := range l.spec.Items(ctx, awsClient) {
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
