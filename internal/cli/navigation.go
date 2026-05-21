package cli

import (
	"github.com/janbaraniewski/synology-ctl/internal/tui"
	"github.com/janbaraniewski/synology-ctl/internal/tui/views"
)

func appSections(vctx tui.ViewContext) []tui.NavSection {
	return []tui.NavSection{
		{Name: "Overview", Views: []tui.View{
			views.NewDashboard(vctx),
			views.NewResourceMonitor(vctx),
		}},
		{Name: "Storage", Views: []tui.View{
			views.NewVolumes(vctx),
			views.NewFiles(vctx),
			views.NewISCSI(vctx),
		}},
		{Name: "Apps", Views: []tui.View{
			views.NewApps(vctx),
			views.NewContainers(vctx),
			views.NewVMM(vctx),
		}},
		{Name: "Backup", Views: []tui.View{
			views.NewHyperBackup(vctx),
			views.NewActiveBackup(vctx),
			views.NewCloudSync(vctx),
		}},
		{Name: "Services", Views: []tui.View{
			views.NewDrive(vctx),
			views.NewSurveillance(vctx),
		}},
		{Name: "Security", Views: []tui.View{
			views.NewCerts(vctx),
			views.NewSecurityAdvisor(vctx),
			views.NewFirewall(vctx),
		}},
		{Name: "System", Views: []tui.View{
			views.NewAdminPage(vctx),
			views.NewQuotas(vctx),
			views.NewSchedTasks(vctx),
			views.NewDDNS(vctx),
			views.NewNotifications(vctx),
		}},
		{Name: "Settings", Views: []tui.View{
			views.NewDSMUpdate(vctx),
			views.NewTimeRegion(vctx),
			views.NewPower(vctx),
			views.NewExternalAccess(vctx),
		}},
		{Name: "Tools", Views: []tui.View{
			views.NewExplorer(vctx),
		}},
	}
}
