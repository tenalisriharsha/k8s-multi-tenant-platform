package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newDeleteCmd() *cobra.Command {
	clusterFl := &clusterFlags{}
	var name string

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Offboard a tenant by deleting its namespace and everything in it",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := clusterFl.newClientset()
			if err != nil {
				return err
			}

			client := tenant.NewClient(clientset)
			if err := client.DeleteTenant(cmd.Context(), name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "tenant %q deleted\n", name)
			return nil
		},
	}

	addClusterFlags(cmd, clusterFl)
	cmd.Flags().StringVar(&name, "name", "", "tenant name to delete (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}
