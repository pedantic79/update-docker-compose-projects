package dockercli

import (
	"context"
	"fmt"
	"maps"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

// GetImageID returns the ImageID via the docker API. Pass it the full image with tag
func GetImageID(ctx context.Context, client *client.Client, imageName string) (string, error) {
	imageInspect, err := client.ImageInspect(ctx, imageName)
	if err != nil {
		return "", err
	}

	return imageInspect.ID, nil
}

func NeedsRestart(ctx context.Context, client *client.Client, images map[string]api.ImageSummary) (bool, error) {
	logs := make([]string, 0, len(images))

	// check if each image is updated
	for i := range maps.Keys(images) {
		image := images[i]

		// Check if the imageID has changed
		imageWithTag := fmt.Sprintf("%s:%s", image.Repository, image.Tag)
		currentImageID, err := GetImageID(ctx, client, imageWithTag)
		if err != nil {
			return false, err
		}

		if currentImageID != image.ID {
			logs = append(logs, fmt.Sprintf("%s has been updated: %s -> %s", image.Repository, image.ID, currentImageID))
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
