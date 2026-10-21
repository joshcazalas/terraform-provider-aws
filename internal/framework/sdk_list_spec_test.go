// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package framework

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"testing"

	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	identityschema "github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
)

const attrName = "name"

type listSpecItem struct {
	id   string
	name string
}

var errListSpec = errors.New("boom")

func listSpecResource(items []listSpecItem, iterErr error, flatten func(listSpecItem, *schema.ResourceData) error, displayName func(listSpecItem) string) *sdkListResourceFromSpec[listSpecItem, struct{}] {
	l := &sdkListResourceFromSpec[listSpecItem, struct{}]{
		spec: SDKListSpec[listSpecItem, struct{}]{
			Items: func(context.Context, *conns.AWSClient, struct{}) (iter.Seq2[listSpecItem, error], diag.Diagnostics) {
				return func(yield func(listSpecItem, error) bool) {
					for _, it := range items {
						if !yield(it, nil) {
							return
						}
					}
					if iterErr != nil {
						yield(listSpecItem{}, iterErr)
					}
				}, nil
			},
			ID: func(it listSpecItem) string { return it.id },
			Flatten: func(_ context.Context, _ *conns.AWSClient, it *listSpecItem, rd *schema.ResourceData) error {
				if flatten == nil {
					return nil
				}

				return flatten(*it, rd)
			},
			DisplayName: displayName,
		},
	}
	l.SetResourceSchema(&schema.Resource{
		Schema: map[string]*schema.Schema{
			attrName: {Type: schema.TypeString, Optional: true},
		},
	})

	return l
}

func listSpecRequest() list.ListRequest {
	return list.ListRequest{
		ResourceSchema: rschema.Schema{
			Attributes: map[string]rschema.Attribute{
				"id":     rschema.StringAttribute{Computed: true},
				attrName: rschema.StringAttribute{Optional: true},
			},
		},
		ResourceIdentitySchema: identityschema.Schema{
			Attributes: map[string]identityschema.Attribute{
				"id": identityschema.StringAttribute{RequiredForImport: true},
			},
		},
	}
}

// collect drains the stream. A positive stopAfter stops the consumer after
// that many successful results.
func collect(t *testing.T, l *sdkListResourceFromSpec[listSpecItem, struct{}], stopAfter int) (displayNames []string, errs int) {
	t.Helper()

	stream := &list.ListResultsStream{}
	l.List(context.Background(), listSpecRequest(), stream)

	if stream.Results == nil {
		t.Fatal("List did not set stream.Results")
	}

	n := 0
	for result := range stream.Results {
		if result.Diagnostics.HasError() {
			errs++

			continue
		}
		displayNames = append(displayNames, result.DisplayName)
		n++
		if stopAfter > 0 && n >= stopAfter {
			break
		}
	}

	return displayNames, errs
}

func TestSDKListSpecEmitsEveryItem(t *testing.T) {
	t.Parallel()

	l := listSpecResource([]listSpecItem{{id: "a"}, {id: "b"}}, nil, nil, nil)

	got, errs := collect(t, l, 0)

	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
	if errs != 0 {
		t.Errorf("got %d error results, want 0", errs)
	}
}

func TestSDKListSpecDisplayNameOverride(t *testing.T) {
	t.Parallel()

	l := listSpecResource([]listSpecItem{{id: "a-id", name: "a-name"}}, nil, nil,
		func(it listSpecItem) string { return it.name })

	got, _ := collect(t, l, 0)

	if want := []string{"a-name"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
}

func TestSDKListSpecIteratorErrorStopsTheStream(t *testing.T) {
	t.Parallel()

	l := listSpecResource([]listSpecItem{{id: "a"}}, errListSpec, nil, nil)

	got, errs := collect(t, l, 0)

	if want := []string{"a"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
	if errs != 1 {
		t.Errorf("got %d error results, want 1", errs)
	}
}

func TestSDKListSpecFlattenErrorSkipsOnlyThatItem(t *testing.T) {
	t.Parallel()

	l := listSpecResource([]listSpecItem{{id: "a"}, {id: "bad"}, {id: "c"}}, nil,
		func(it listSpecItem, _ *schema.ResourceData) error {
			if it.id == "bad" {
				return errListSpec
			}

			return nil
		}, nil)

	got, errs := collect(t, l, 0)

	if want := []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
	if errs != 0 {
		t.Errorf("a flatten error must not fail the list, got %d error results", errs)
	}
}

func TestSDKListSpecClearedIDIsSkipped(t *testing.T) {
	t.Parallel()

	l := listSpecResource([]listSpecItem{{id: "a"}, {id: "gone"}, {id: "c"}}, nil,
		func(it listSpecItem, rd *schema.ResourceData) error {
			if it.id == "gone" {
				rd.SetId("")
			}

			return nil
		}, nil)

	got, errs := collect(t, l, 0)

	if want := []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
	if errs != 0 {
		t.Errorf("got %d error results, want 0", errs)
	}
}

func TestSDKListSpecStopsWhenConsumerStops(t *testing.T) {
	t.Parallel()

	var flattened int
	l := listSpecResource([]listSpecItem{{id: "a"}, {id: "b"}, {id: "c"}}, nil,
		func(listSpecItem, *schema.ResourceData) error {
			flattened++

			return nil
		}, nil)

	got, _ := collect(t, l, 1)

	if len(got) != 1 {
		t.Errorf("collected %d results, want 1", len(got))
	}
	if flattened != 1 {
		t.Errorf("flatten called %d times after an early stop, want 1", flattened)
	}
}

type listSpecQuery struct {
	Prefix types.String `tfsdk:"prefix"`
}

func listSpecQuerySchema(context.Context) listschema.Schema {
	return listschema.Schema{
		Attributes: map[string]listschema.Attribute{
			"prefix": listschema.StringAttribute{Optional: true},
		},
	}
}

// querySpecResource lists only items whose ID carries the configured prefix.
func querySpecResource(items []listSpecItem, setupDiags diag.Diagnostics) *sdkListResourceFromSpec[listSpecItem, listSpecQuery] {
	l := &sdkListResourceFromSpec[listSpecItem, listSpecQuery]{
		spec: SDKListSpec[listSpecItem, listSpecQuery]{
			ConfigSchema: listSpecQuerySchema,
			Items: func(_ context.Context, _ *conns.AWSClient, query listSpecQuery) (iter.Seq2[listSpecItem, error], diag.Diagnostics) {
				if setupDiags.HasError() {
					return nil, setupDiags
				}

				return func(yield func(listSpecItem, error) bool) {
					for _, it := range items {
						if !strings.HasPrefix(it.id, query.Prefix.ValueString()) {
							continue
						}
						if !yield(it, nil) {
							return
						}
					}
				}, nil
			},
			ID:      func(it listSpecItem) string { return it.id },
			Flatten: func(context.Context, *conns.AWSClient, *listSpecItem, *schema.ResourceData) error { return nil },
		},
	}
	l.SetResourceSchema(&schema.Resource{
		Schema: map[string]*schema.Schema{
			attrName: {Type: schema.TypeString, Optional: true},
		},
	})

	return l
}

func queryRequest(t *testing.T, prefix string) list.ListRequest {
	t.Helper()

	req := listSpecRequest()
	sch := listSpecQuerySchema(context.Background())
	req.Config = tfsdk.Config{
		Schema: sch,
		Raw: tftypes.NewValue(sch.Type().TerraformType(context.Background()), map[string]tftypes.Value{
			"prefix": tftypes.NewValue(tftypes.String, prefix),
		}),
	}

	return req
}

func TestSDKListSpecDecodesTheQuery(t *testing.T) {
	t.Parallel()

	l := querySpecResource([]listSpecItem{{id: "keep-a"}, {id: "drop-b"}, {id: "keep-c"}}, nil)

	stream := &list.ListResultsStream{}
	l.List(context.Background(), queryRequest(t, "keep-"), stream)

	var got []string
	for result := range stream.Results {
		if result.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
		}
		got = append(got, result.DisplayName)
	}

	if want := []string{"keep-a", "keep-c"}; !slices.Equal(got, want) {
		t.Errorf("display names = %v, want %v", got, want)
	}
}

// A setup failure has to surface before any result, not as a per-item error.
func TestSDKListSpecReportsSetupDiagnostics(t *testing.T) {
	t.Parallel()

	var setupDiags diag.Diagnostics
	setupDiags.AddError("bad query", "cannot build the input")

	l := querySpecResource([]listSpecItem{{id: "keep-a"}}, setupDiags)

	stream := &list.ListResultsStream{}
	l.List(context.Background(), queryRequest(t, "keep-"), stream)

	var results, errs int
	for result := range stream.Results {
		results++
		if result.Diagnostics.HasError() {
			errs++
		}
	}

	if results != 1 || errs != 1 {
		t.Errorf("got %d results with %d carrying errors, want exactly 1 error result", results, errs)
	}
}

func TestSDKListSpecConfigSchemaDefaultsToEmpty(t *testing.T) {
	t.Parallel()

	withQuery := querySpecResource(nil, nil)
	noQuery := listSpecResource(nil, nil, nil, nil)

	var response list.ListResourceSchemaResponse
	withQuery.ListResourceConfigSchema(context.Background(), list.ListResourceSchemaRequest{}, &response)
	if _, ok := response.Schema.Attributes["prefix"]; !ok {
		t.Error("declared config schema was not used")
	}

	response = list.ListResourceSchemaResponse{}
	noQuery.ListResourceConfigSchema(context.Background(), list.ListResourceSchemaRequest{}, &response)
	if n := len(response.Schema.Attributes); n != 0 {
		t.Errorf("default config schema has %d attributes, want 0", n)
	}
}
