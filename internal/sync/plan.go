package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattjackson/basement/internal/driver"
)

// Action represents a single object operation in a sync job.
type Action struct {
	SrcBucket  string
	SrcKey     string
	DstBucket  string
	DstKey     string
	ActionType string // "copy" | "skip"
	Reason     string // for skip actions: "etag_match", etc.
	ObjectInfo *driver.ObjectInfo
}

// Plan walks the source bucket and returns a list of copy/skip actions.
func Plan(ctx context.Context, srcDriver driver.Driver, dstDriver driver.Driver, srcConnID, srcBucket, srcPrefix, dstConnID, dstBucket, dstPrefix string) ([]Action, error) {
	var actions []Action

	// Walk source bucket in pages
	var continuation string
	prefixLen := len(srcPrefix)
	if prefixLen > 0 && !strings.HasSuffix(srcPrefix, "/") {
		srcPrefix = srcPrefix + "/"
	}

	for {
		// Sync engine wants a flat recursive list across every key
		// under srcPrefix, so we pass delimiter="" (the disable signal).
		page, err := srcDriver.ListObjects(ctx, srcBucket, srcPrefix, continuation, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("listing source objects: %w", err)
		}

		for _, obj := range page.Objects {
			// Skip directories
			if obj.IsDir {
				continue
			}

			srcKey := obj.Key
			dstKey := srcKey
			if prefixLen > 0 {
				relativeKey := strings.TrimPrefix(srcKey, srcPrefix)
				if relativeKey != "" && dstPrefix != "" {
					dstKey = dstPrefix + "/" + relativeKey
				} else if relativeKey != "" {
					dstKey = relativeKey
				}
			}

			// Check if destination object exists and has same ETag
			dstObj, err := dstDriver.StatObject(ctx, dstBucket, dstKey)
			if err == nil && dstObj.ETag == obj.ETag {
				actions = append(actions, Action{
					SrcBucket:  srcBucket,
					SrcKey:     srcKey,
					DstBucket:  dstBucket,
					DstKey:     dstKey,
					ActionType: "skip",
					Reason:     "etag_match",
					ObjectInfo: &obj,
				})
			} else {
				actions = append(actions, Action{
					SrcBucket:  srcBucket,
					SrcKey:     srcKey,
					DstBucket:  dstBucket,
					DstKey:     dstKey,
					ActionType: "copy",
					ObjectInfo: &obj,
				})
			}
		}

		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		continuation = page.NextContinuation
	}

	return actions, nil
}
