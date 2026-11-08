package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newCreateCmd() *cobra.Command {
	specFl := &specFlags{}
	clusterFl := &clusterFlags{}
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Onboard a new tenant: create its namespace, quota, limits, network policy, and RBAC",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := specFl.resolveSpec()
			if err != nil {
				return err
			}
			if err := tenant.Validate(spec); err != nil {
				return err
			}

			bundle := tenant.Build(spec)

			if dryRun {
				yamlBytes, err := tenant.RenderYAML(bundle)
				if err != nil {
					return fmt.Errorf("render manifests: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "# dry run: nothing applied to the cluster")
				_, err = cmd.OutOrStdout().Write(yamlBytes)
				return err
			}

			clientset, err := clusterFl.newClientset()
			if err != nil {
				return err
			}

			client := tenant.NewClient(clientset)
			if err := client.CreateTenant(cmd.Context(), bundle); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "tenant %q created: namespace, quota, limit range, %d network policies, and RBAC applied\n", spec.Name, len(bundle.NetworkPolicies))
			return nil
		},
	}

	addSpecFlags(cmd, specFl)
	addClusterFlags(cmd, clusterFl)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render the bundle that would be applied and exit, without contacting the cluster")

	return cmd
}
