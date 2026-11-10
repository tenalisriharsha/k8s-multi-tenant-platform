package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newUpdateCmd() *cobra.Command {
	specFl := &specFlags{}
	clusterFl := &clusterFlags{}
	var dryRun bool
	var diff bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Reconcile an existing tenant's namespace labels, quota, limits, network policy, and RBAC to match a spec",
		Long: "Build the full desired bundle from the given spec (the same flags, or --file,\n" +
			"as `create`) and reconcile an existing tenant's namespace labels (e.g. its Pod\n" +
			"Security Admission level), quota, limit range, network policies, and RBAC to\n" +
			"match it. The namespace itself is never deleted or recreated, only its labels\n" +
			"are updated. Fails if the tenant does not exist or is not tenantctl-managed.",
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

			if diff {
				out, err := client.DiffTenant(cmd.Context(), bundle)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), out)
				return nil
			}

			if err := client.UpdateTenant(cmd.Context(), bundle); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "tenant %q updated: namespace (pod-security=%s), quota, limit range, %d network policies, and RBAC reconciled\n", spec.Name, podSecurityLabel(spec), len(bundle.NetworkPolicies))
			return nil
		},
	}

	addSpecFlags(cmd, specFl)
	addClusterFlags(cmd, clusterFl)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render the desired bundle and exit, without contacting the cluster")
	cmd.Flags().BoolVar(&diff, "diff", false, "show a line diff between the live cluster state and the desired bundle, without applying changes")

	return cmd
}
