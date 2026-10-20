// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package iter

import (
	"context"
	"fmt"
	"iter"
)

// Paginator is an AWS SDK for Go v2 paginator over pages of type O, where OPT
// is the service's Options type. The SDK's generated paginators satisfy it.
type Paginator[O, OPT any] interface {
	HasMorePages() bool
	NextPage(context.Context, ...func(*OPT)) (*O, error)
}

// PageItems returns an iterator over the items of every page produced by p.
// items extracts one page's item slice. The first error is yielded wrapped as
// "what: err" and iteration stops.
//
// OPT cannot be inferred when no option functions are passed, so give it
// explicitly and let O and T be inferred from items:
//
//	tfiter.PageItems[ecr.Options](ctx, "listing ECR Repository resources",
//		ecr.NewDescribeRepositoriesPaginator(conn, input),
//		func(o *ecr.DescribeRepositoriesOutput) []awstypes.Repository { return o.Repositories })
//
// The returned sequence is single-use: p carries the pagination state, so a
// second range over the same sequence yields nothing. Call PageItems again to
// start over.
func PageItems[OPT, O, T any](ctx context.Context, what string, p Paginator[O, OPT], items func(*O) []T, optFns ...func(*OPT)) iter.Seq2[T, error] {
	return ConcatValuesWithError(pages(ctx, what, p, items, optFns...))
}

// pages returns an iterator over the item slice of each page produced by p.
func pages[OPT, O, T any](ctx context.Context, what string, p Paginator[O, OPT], items func(*O) []T, optFns ...func(*OPT)) iter.Seq2[[]T, error] {
	return func(yield func([]T, error) bool) {
		for p.HasMorePages() {
			page, err := p.NextPage(ctx, optFns...)
			if err != nil {
				yield(nil, fmt.Errorf("%s: %w", what, err))

				return
			}

			if !yield(items(page), nil) {
				return
			}
		}
	}
}
