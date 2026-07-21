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
		// Whenever you bump the version, also update the REQUIRED_VERSION in
		// changelog/Makefile in the EE repository.
		Version: "2.1.0",

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
