package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func newListCmd() *cobra.Command {
	clusterFl := &clusterFlags{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every tenant-platform-managed tenant on the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := clusterFl.newClientset()
			if err != nil {
				return err
			}

			client := tenant.NewClient(clientset)
			tenants, err := client.ListTenants(cmd.Context())
			if err != nil {
				return err
			}

			if len(tenants) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no tenants found")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPHASE\tCREATED")
			for _, t := range tenants {
				fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Phase, t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			return w.Flush()
		},
	}

	addClusterFlags(cmd, clusterFl)
	return cmd
}
