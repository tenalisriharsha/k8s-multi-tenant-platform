package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newGenerateCmd() *cobra.Command {
	flags := &specFlags{}
	var output string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Render a tenant's manifest bundle to YAML without touching a cluster",
		Long: "Render the Namespace, ResourceQuota, LimitRange, NetworkPolicies, Role, and\n" +
			"RoleBinding for a tenant as a single multi-document YAML stream. Useful for\n" +
			"reviewing what `create` would apply, or for piping into `kubectl apply -f -`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := flags.toSpec()
			if err := tenant.Validate(spec); err != nil {
				return err
			}

			bundle := tenant.Build(spec)
			yamlBytes, err := tenant.RenderYAML(bundle)
			if err != nil {
				return fmt.Errorf("render manifests: %w", err)
			}

			if output == "" {
				_, err = cmd.OutOrStdout().Write(yamlBytes)
				return err
			}
			return os.WriteFile(output, yamlBytes, 0o644)
		},
	}

	addSpecFlags(cmd, flags)
	cmd.Flags().StringVar(&output, "output", "", "write YAML to this file instead of stdout")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("admin-name")

	return cmd
}
