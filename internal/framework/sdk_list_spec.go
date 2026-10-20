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
	"github.com/hashicorp/terraform-provider-aws/names"
)

// ItemErrorPolicy decides what a shared List does when one remote object
// cannot be flattened. Declarative policy; the loop itself is behavior.
type ItemErrorPolicy int

const (
	// SkipItem logs and continues. Default: one bad object does not fail the list.
	SkipItem ItemErrorPolicy = iota
	// FailList aborts the stream with a diagnostic.
	FailList
)

// SDKListSpec is the declarative model for a paginated list over an SDKv2
// resource whose list call returns whole objects.
//
// Item is inferred from Items and is never authored.
type SDKListSpec[Item any] struct {
	// Items binds the service's page iterator. Required.
	Items func(context.Context, *conns.AWSClient) iter.Seq2[Item, error]

	// ID extracts the Terraform resource ID. Required.
	ID func(Item) string

	// Flatten binds the resource's flatten function. Required.
	//
	// The parameter order matches the provider's prevailing flatten contract,
	// func(ctx, item, d) error, which 24 of the 40 shape-A list resources
	// already use.
	Flatten func(context.Context, *Item, *schema.ResourceData) error

	// DisplayName defaults to ID when nil.
	DisplayName func(Item) string

	// OnItemError defaults to SkipItem.
	OnItemError ItemErrorPolicy
}

// SDKListResourceFromSpec is the single shared implementation.
type SDKListResourceFromSpec[Item any] struct {
	ListResourceWithSDKv2Resource
	spec SDKListSpec[Item]
}

// NewSDKListResource builds a list resource from a schema and a spec.
func NewSDKListResource[Item any](res *schema.Resource, spec SDKListSpec[Item]) *SDKListResourceFromSpec[Item] {
	l := &SDKListResourceFromSpec[Item]{spec: spec}
	l.SetResourceSchema(res)

	return l
}

func (l *SDKListResourceFromSpec[Item]) List(ctx context.Context, request list.ListRequest, stream *list.ListResultsStream) {
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

			if err := l.spec.Flatten(ctx, &item, rd); err != nil {
				if l.spec.OnItemError == SkipItem {
					tflog.Error(ctx, "listing: skipping item", map[string]any{
						names.AttrID: id,
						"error":      err.Error(),
					})

					continue
				}

				yield(fwdiag.NewListResultErrorDiagnostic(err))

				return
			}

			// Logically deleted: flatten cleared the ID.
			if rd.Id() == "" {
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
