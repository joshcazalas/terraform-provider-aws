// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package kms

import (
	"context"
	"fmt"
	"iter"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	inttypes "github.com/hashicorp/terraform-provider-aws/internal/types"
)

// @SDKListResource("aws_kms_alias")
func aliasResourceAsListResource() inttypes.ListResourceForSDK {
	return framework.NewSDKListResource(resourceAlias(), framework.SDKListSpec[awstypes.AliasListEntry]{
		Items: func(ctx context.Context, c *conns.AWSClient) iter.Seq2[awstypes.AliasListEntry, error] {
			var input kms.ListAliasesInput

			return listAliases(ctx, c.KMSClient(ctx), &input)
		},
		ID:      func(a awstypes.AliasListEntry) string { return aws.ToString(a.AliasName) },
		Flatten: resourceAliasFlatten,
	})
}

func listAliases(ctx context.Context, conn *kms.Client, input *kms.ListAliasesInput) iter.Seq2[awstypes.AliasListEntry, error] {
	return func(yield func(awstypes.AliasListEntry, error) bool) {
		pages := kms.NewListAliasesPaginator(conn, input)
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				yield(awstypes.AliasListEntry{}, fmt.Errorf("listing KMS Aliases resources: %w", err))
				return
			}

			for _, item := range page.Aliases {
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}
