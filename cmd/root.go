package cmd

import (
	"github.com/urfave/cli/v2"
)

// global flags
var (
	debug bool
)

func New() *cli.App {
	app := &cli.App{
		Name:        "changelog",
		Usage:       "changelog [command]",
		Description: "A changelog tool to manage changelog.",
		// whenever you bump the version, please update the
		Version: "2.1.0", // "REQUIRED_VERSION" of changelog/Makefile in EE repo

		// global flags
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "debug",
				Usage:       "debug mode",
				Required:    false,
				Destination: &debug,
			},
		},

		// commands
		Commands: []*cli.Command{
			newGenerateCmd(),
		},
	}

	return app
}
