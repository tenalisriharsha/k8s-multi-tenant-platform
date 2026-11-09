package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newStatusCmd() *cobra.Command {
	clusterFl := &clusterFlags{}

	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show live ResourceQuota usage vs. limits for one tenant, or every tenant",
		Long: "Report each tenant's observed resource consumption (ResourceQuota.Status.Used)\n" +
			"against its hard limit, as a percentage. With no argument, reports every\n" +
			"tenantctl-managed tenant; with a tenant name, reports just that tenant and\n" +
			"fails if it does not exist or is not tenantctl-managed.\n\n" +
			"This is read-only and distinct from `update --diff`: `status` reports quota\n" +
			"usage numbers, not full-bundle spec-vs-live drift.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := clusterFl.newClientset()
			if err != nil {
				return err
			}
			client := tenant.NewClient(clientset)

			var tenants []tenant.Info
			if len(args) == 1 {
				info, err := client.GetTenant(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				tenants = []tenant.Info{info}
			} else {
				tenants, err = client.ListTenants(cmd.Context())
				if err != nil {
					return err
				}
			}

			if len(tenants) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no tenants found")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TENANT\tRESOURCE\tUSED\tHARD\tUTILIZATION")
			for _, t := range tenants {
				if len(t.Usage) == 0 {
					fmt.Fprintf(w, "%s\t-\t-\t-\tno quota data\n", t.Name)
					continue
				}
				for _, u := range t.Usage {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d%%\n", t.Name, u.Resource, u.Used.String(), u.Hard.String(), u.Percent())
				}
			}
			return w.Flush()
		},
	}

	addClusterFlags(cmd, clusterFl)
	return cmd
}
