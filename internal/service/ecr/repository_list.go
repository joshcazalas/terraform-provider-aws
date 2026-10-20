// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package ecr

import (
	"context"
	"fmt"
	"iter"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	awstypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	inttypes "github.com/hashicorp/terraform-provider-aws/internal/types"
)

// @SDKListResource("aws_ecr_repository")
func repositoryResourceAsListResource() inttypes.ListResourceForSDK {
	return framework.NewSDKListResource(resourceRepository(), framework.SDKListSpec[awstypes.Repository]{
		Items: func(ctx context.Context, c *conns.AWSClient) iter.Seq2[awstypes.Repository, error] {
			var input ecr.DescribeRepositoriesInput

			return listRepositories(ctx, c.ECRClient(ctx), &input)
		},
		ID:      func(r awstypes.Repository) string { return aws.ToString(r.RepositoryName) },
		Flatten: resourceRepositoryFlatten,
	})
}

func listRepositories(ctx context.Context, conn *ecr.Client, input *ecr.DescribeRepositoriesInput) iter.Seq2[awstypes.Repository, error] {
	return func(yield func(awstypes.Repository, error) bool) {
		pages := ecr.NewDescribeRepositoriesPaginator(conn, input)
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				yield(awstypes.Repository{}, fmt.Errorf("listing ECR Repository resources: %w", err))
				return
			}

			for _, item := range page.Repositories {
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}
