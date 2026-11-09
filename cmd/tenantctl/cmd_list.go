package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

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
			fmt.Fprintln(w, "NAME\tPHASE\tCREATED\tQUOTA")
			for _, t := range tenants {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Name, t.Phase, t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), peakUtilization(t.Usage))
			}
			return w.Flush()
		},
	}

	addClusterFlags(cmd, clusterFl)
	return cmd
}

// peakUtilization summarizes a tenant's quota usage as its single
// most-utilized resource, e.g. "memory 72%". Full per-resource usage is
// available via `tenantctl status`. Returns "-" when no quota data is
// available (e.g. the namespace is still being created).
func peakUtilization(usage []tenant.ResourceUsage) string {
	if len(usage) == 0 {
		return "-"
	}
	peak := usage[0]
	for _, u := range usage[1:] {
		if u.Percent() > peak.Percent() {
			peak = u
		}
	}
	return fmt.Sprintf("%s %d%%", shortResourceName(peak.Resource), peak.Percent())
}

// shortResourceName strips the quota-key prefix ("requests." or "limits.")
// from a ResourceQuota key, e.g. "requests.memory" -> "memory", so the
// QUOTA column reads as a bare resource name regardless of which quota
// counted toward it.
func shortResourceName(name corev1.ResourceName) string {
	s := string(name)
	if i := strings.LastIndex(s, "."); i != -1 {
		return s[i+1:]
	}
	return s
}
