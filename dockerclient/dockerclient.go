package dockerclient

import (
	"context"
	"fmt"
	"maps"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

func NeedsRestart(ctx context.Context, cli *client.Client, originalImages map[string]api.ImageSummary) (bool, error) {
	logs := make([]string, 0, len(originalImages))

	// check if each image is updated
	for oImage := range maps.Values(originalImages) {

		// Check if the imageID has changed
		imageWithTag := fmt.Sprintf("%s:%s", oImage.Repository, oImage.Tag)
		currentImage, err := cli.ImageInspect(ctx, imageWithTag)
		if err != nil {
			return false, err
		}

		if currentImage.ID != oImage.ID {
			logs = append(logs, fmt.Sprintf("%s has been updated: %s -> %s", oImage.Repository, oImage.ID, currentImage.ID))
		}
	}

	for _, log := range logs {
		fmt.Println(log)
	}

	return len(logs) > 0, nil
}

func ImagePrune(ctx context.Context, cli *client.Client) (client.ImagePruneResult, error) {
	fmt.Println("Pruning images...")
	return cli.ImagePrune(ctx, client.ImagePruneOptions{})
}
