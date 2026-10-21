// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package ec2

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// listDisplayName labels a list result "<Name tag> (<id>)" when the object
// carries a Name tag, and the id alone otherwise. EC2 objects are identified by
// an opaque id, so the tag is what a reader recognizes.
func listDisplayName(tags []awstypes.Tag, id string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == "Name" {
			return fmt.Sprintf("%s (%s)", aws.ToString(tag.Value), id)
		}
	}

	return id
}
