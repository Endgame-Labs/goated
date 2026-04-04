package main

import (
	"fmt"

	"toolbox-example/internal/creds"

	"github.com/spf13/cobra"
)

func credsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "creds",
		Short: "Read credentials through workspace/goat",
	}
	cmd.AddCommand(credsGetCmd())
	return cmd
}

func credsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [KEY]",
		Short: "Fetch a credential by key name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value, err := creds.Get(args[0])
			if err != nil {
				return err
			}
			fmt.Println(value)
			return nil
		},
	}
}
