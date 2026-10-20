// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package framework

import (
	"context"
	"errors"
	"iter"
	"slices"
	"testing"

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

// listSpecResource builds a list resource over the given items, with a flatten
// that applies f to each item's ResourceData.
func listSpecResource(items []listSpecItem, iterErr error, flatten func(listSpecItem, *schema.ResourceData) error, displayName func(listSpecItem) string) *sdkListResourceFromSpec[listSpecItem] {
	l := &sdkListResourceFromSpec[listSpecItem]{
		spec: SDKListSpec[listSpecItem]{
			Items: func(context.Context, *conns.AWSClient) iter.Seq2[listSpecItem, error] {
				return func(yield func(listSpecItem, error) bool) {
					for _, it := range items {
						if !yield(it, nil) {
							return
						}
					}
					if iterErr != nil {
						yield(listSpecItem{}, iterErr)
					}
				}
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

// collect drains the stream, returning display names of successful results and
// the number of error results.
func collect(t *testing.T, l *sdkListResourceFromSpec[listSpecItem], stopAfter int) (displayNames []string, errs int) {
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

// A flatten that clears the ID means the object is logically deleted.
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
