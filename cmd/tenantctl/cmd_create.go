package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newCreateCmd() *cobra.Command {
	specFl := &specFlags{}
	clusterFl := &clusterFlags{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Onboard a new tenant: create its namespace, quota, limits, network policy, and RBAC",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := specFl.toSpec()
			if err := tenant.Validate(spec); err != nil {
				return err
			}

			clientset, err := clusterFl.newClientset()
			if err != nil {
				return err
			}

			bundle := tenant.Build(spec)
			client := tenant.NewClient(clientset)
			if err := client.CreateTenant(cmd.Context(), bundle); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "tenant %q created: namespace, quota, limit range, 3 network policies, and RBAC applied\n", spec.Name)
			return nil
		},
	}

	addSpecFlags(cmd, specFl)
	addClusterFlags(cmd, clusterFl)
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("admin-name")

	return cmd
}
