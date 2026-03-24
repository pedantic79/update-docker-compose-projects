package utils

import (
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
)

func NewDockerCli() (*command.DockerCli, error) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}

	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, err
	}

	return dockerCli, nil
}
