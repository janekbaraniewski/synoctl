package demo

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// handlerFn is one demo endpoint. It receives the parsed request form and
// returns whatever should be placed under `data` in the success envelope.
// Returning nil produces an empty data object — DSM tolerates that.
type handlerFn func(s *Server, form url.Values) any

// handlers returns the api:method → handler map. Built lazily because some
// of the closure-captured demo data lives in data.go and we want the
// state-aware ones to close over the server's mutable store.
func (s *Server) handlers() map[string]handlerFn {
	return map[string]handlerFn{
		// — auth + introspection —
		"SYNO.API.Auth:login":  s.handleLogin,
		"SYNO.API.Auth:logout": s.handleLogout,
		"SYNO.API.Info:query":  s.handleAPIInfo,

		// — system —
		"SYNO.Core.System:info":            s.handleSystemInfo,
		"SYNO.Core.System.Utilization:get": s.handleUtilization,
		"SYNO.Core.System:reboot":          noopOK,
		"SYNO.Core.System:shutdown":        noopOK,
		"SYNO.Core.System.Process:list":    s.handleProcessList,
		"SYNO.Core.System.Process:get":     s.handleProcessList,

		// — storage —
		"SYNO.Storage.CGI.Storage:load_info": s.handleStorage,

		// — shares —
		"SYNO.Core.Share:list":            s.handleShares,
		"SYNO.Core.Share.Snapshot:list":   s.handleSnapshotsList,
		"SYNO.Core.Share.Snapshot:create": s.handleSnapshotCreate,
		"SYNO.Core.Share.Snapshot:delete": s.handleSnapshotDelete,

		// — file station —
		"SYNO.FileStation.List:list_share": s.handleFileShares,
		"SYNO.FileStation.List:list":       s.handleFileList,
		"SYNO.FileStation.DirSize:start":   s.handleDirSizeStart,
		"SYNO.FileStation.DirSize:status":  s.handleDirSizeStatus,
		"SYNO.FileStation.Delete:delete":   noopOK,
		"SYNO.FileStation.Rename:rename":   noopOK,

		// — users / groups —
		"SYNO.Core.User:list":       s.handleUsers,
		"SYNO.Core.User:create":     noopOK,
		"SYNO.Core.User:set":        noopOK,
		"SYNO.Core.User:delete":     noopOK,
		"SYNO.Core.User.Group:list": s.handleGroups,

		// — network / logs —
		"SYNO.Core.Network.Interface:list": s.handleNetwork,
		"SYNO.Core.SyslogClient.Log:list":  s.handleLogs,

		// — packages + services —
		"SYNO.Core.Package:list":                     s.handlePackages,
		"SYNO.Core.Package.Server:list":              s.handlePackageCatalog,
		"SYNO.Core.Package.Uninstallation:uninstall": noopOK,
		"SYNO.Core.Package.Installation:check":       s.handleInstallCheck,
		"SYNO.Core.Package.Installation:get_queue":   s.handleInstallQueue,
		"SYNO.Core.Package.Installation:install":     s.handleInstallPackage,
		"SYNO.Core.Package.Installation:start":       s.handleInstallStart,
		"SYNO.Core.Package.Installation:status":      s.handleInstallStatus,
		"SYNO.Core.Package.Installation:end":         noopOK,
		"SYNO.Core.Package.Control:start":            s.handlePkgControl("running"),
		"SYNO.Core.Package.Control:stop":             s.handlePkgControl("stop"),
		"SYNO.Core.Service:list":                     s.handleServices,
		"SYNO.Core.Service:get":                      s.handleServices,
		"SYNO.Core.Service:set":                      noopOK,

		// — containers (docker) —
		"SYNO.Docker.Container:list": s.handleContainers,
		"SYNO.Docker.Image:list":     s.handleDockerImages,
		"SYNO.Docker.Network:list":   s.handleDockerNetworks,

		// — surveillance —
		"SYNO.SurveillanceStation.Camera:List":    s.handleCameras,
		"SYNO.SurveillanceStation.Recording:List": s.handleRecordings,
		"SYNO.SurveillanceStation.Info:GetInfo":   s.handleSurveillanceInfo,

		// — backup —
		"SYNO.Backup.Task:list":          s.handleHyperBackup,
		"SYNO.Backup.Task:backup_now":    noopOK,
		"SYNO.Backup.Task:suspend":       noopOK,
		"SYNO.Backup.Task:resume":        noopOK,
		"SYNO.ActiveBackup.Task:list":    s.handleActiveBackup,
		"SYNO.ActiveBackup.Task:backup":  noopOK,
		"SYNO.ActiveBackup.Task:cancel":  noopOK,
		"SYNO.ActiveBackup.Version:list": s.handleActiveBackupVersions,
		"SYNO.CloudSync:list":            s.handleCloudSync,

		// — drive —
		"SYNO.SynologyDrive.Files:list":      s.handleDriveFiles,
		"SYNO.SynologyDrive.Files.Stats:get": s.handleDriveStats,

		// — security —
		"SYNO.Core.Certificate:list":               s.handleCerts,
		"SYNO.Core.SecurityAdvisor.Conf:get":       s.handleSecAdvisorConf,
		"SYNO.Core.SecurityAdvisor.Checklist:list": s.handleSecAdvisorItems,
		"SYNO.Core.TaskScheduler:list":             s.handleSchedTasks,
		"SYNO.Core.TaskScheduler:run":              noopOK,
		"SYNO.Core.TaskScheduler:set":              noopOK,
		"SYNO.Core.Network.Firewall.Conf:get":      s.handleFirewallStatus,
		"SYNO.Core.Network.Firewall.Profile:list":  s.handleFirewallProfiles,
		"SYNO.Core.Network.Firewall.Rules:list":    s.handleFirewallRules,
		"SYNO.Core.Network.Firewall.Rules:add":     noopOK,
		"SYNO.Core.Network.Firewall.Rules:delete":  noopOK,
		"SYNO.Core.Network.Firewall.Rules:set":     noopOK,
		"SYNO.Core.DDNS.Provider:list":             s.handleDDNSProviders,
		"SYNO.Core.DDNS.Record:list":               s.handleDDNSRecords,
		"SYNO.Core.DDNS.Record:set":                noopOK,
		"SYNO.Core.DDNS.Record:delete":             noopOK,
		"SYNO.Core.DDNS.ExtIP:list":                s.handleDDNSExtIP,

		// — notifications —
		"SYNO.Core.Notification.Service:get":  s.handleNotificationSettings,
		"SYNO.Core.Notification.History:list": s.handleNotificationLog,

		// — quotas (share quotas come from SYNO.Core.Share:list above) —
		"SYNO.Core.User.Quota:list": s.handleUserQuotas,

		// — virtual machine manager —
		"SYNO.Virtualization.Guest:list": s.handleVMs,
		"SYNO.Virtualization.Host:list":  s.handleVMHosts,

		// — iscsi / san manager —
		"SYNO.Core.ISCSI.Target:list": s.handleISCSITargets,
		"SYNO.Core.ISCSI.LUN:list":    s.handleISCSILUNs,

		// — settings: DSM update / time-region / power / external-access —
		"SYNO.Core.Upgrade.Server:check":        s.handleDSMUpdate,
		"SYNO.Core.Upgrade:download_status":     s.handleDSMDownloadStatus,
		"SYNO.Core.Region.NTP:get":              s.handleRegionNTP,
		"SYNO.Core.Region.Language:get":         s.handleRegionLanguage,
		"SYNO.Core.Hardware.PowerSchedule:list": s.handlePowerSchedule,
		"SYNO.Core.Hardware.WOL:get":            s.handleWakeOnLAN,
		"SYNO.Core.QuickConnect:get":            s.handleQuickConnect,
		"SYNO.Core.PortForwarding:list":         s.handlePortForwarding,
	}
}

func noopOK(_ *Server, _ url.Values) any { return map[string]any{} }

// — auth —

func (s *Server) handleLogin(_ *Server, _ url.Values) any {
	return map[string]any{
		"sid":       "demo-session-sid",
		"did":       "demo-device-token",
		"device_id": "demo-device-token", // both names, per the DSM 7.0.1 quirk we wrote about
		"synotoken": "demo-csrf",
		"account":   "operator",
	}
}

func (s *Server) handleLogout(_ *Server, _ url.Values) any { return map[string]any{} }

func (s *Server) handleAPIInfo(_ *Server, _ url.Values) any {
	// Advertise every API we serve so c.Supports() returns true and
	// the views render their full content instead of empty-states.
	out := map[string]map[string]any{}
	for k := range s.handlers() {
		api := strings.SplitN(k, ":", 2)[0]
		out[api] = map[string]any{
			"path":       "entry.cgi",
			"minVersion": 1,
			"maxVersion": 6,
		}
	}
	out["SYNO.API.Auth"] = map[string]any{
		"path":       "auth.cgi",
		"minVersion": 1,
		"maxVersion": 7,
	}
	return out
}

// — system —

func (s *Server) handleSystemInfo(_ *Server, _ url.Values) any {
	return map[string]any{
		"model":               "DS923+",
		"serial":              "23B0DEMO123XYZ",
		"firmware_ver":        "DSM 7.2.2-72806 Update 3",
		"firmware_date":       "2026-04-12",
		"ntp_server":          "pool.ntp.org",
		"enabled_ntp":         true,
		"time_zone":           "Europe/Warsaw",
		"time_zone_desc":      "(GMT+02:00) Warsaw",
		"sys_temp":            38,
		"temperature_warning": false,
		"up_time":             s.uptimeStringDSM(),
		"time":                time.Now().Format("2006-01-02 15:04:05"),
		"cpu_clock_speed":     2600,
		"cpu_cores":           "2",
		"cpu_family":          "Ryzen R1600",
		"cpu_series":          "Embedded",
		"cpu_vendor":          "AMD",
		"ram_size":            8192,
		"support_esata":       "no",
	}
}

func (s *Server) handleUtilization(_ *Server, form url.Values) any {
	// SYNO.Core.System.Utilization.get takes a `type` parameter that
	// selects between the live sample ("current", or unset) and one of
	// the historical windows ("hour", "day", "week", "month", "year").
	// The historical case returns an array of per-slot samples so the
	// Resource Monitor view can draw a sparkline.
	switch form.Get("type") {
	case "", "current":
		return s.demoLiveUtilization()
	case "hour":
		return s.demoUtilSeries(60)
	case "day":
		return s.demoUtilSeries(96)
	case "week":
		return s.demoUtilSeries(84)
	case "month":
		return s.demoUtilSeries(60)
	case "year":
		return s.demoUtilSeries(52)
	default:
		return s.demoLiveUtilization()
	}
}

// demoLiveUtilization returns a single in-the-moment utilisation sample
// with the "alive but not stressed" bias.
func (s *Server) demoLiveUtilization() map[string]any {
	cpuUser := 8 + s.intN(12)
	cpuSys := 4 + s.intN(6)
	cpuOther := 2 + s.intN(4)
	memUse := 58 + s.intN(8) // %
	rx := int64(2_500_000 + s.intN(8_000_000))
	tx := int64(400_000 + s.intN(1_200_000))
	diskUtil := 18 + s.intN(40)
	return map[string]any{
		"cpu": map[string]any{
			"1min_load":   17,
			"5min_load":   22,
			"15min_load":  19,
			"user_load":   cpuUser,
			"system_load": cpuSys,
			"other_load":  cpuOther,
			"device":      "System",
		},
		"memory": map[string]any{
			"avail_real":  2_950_000,
			"avail_swap":  2_097_152,
			"buffer":      384_000,
			"cached":      1_584_000,
			"memory_size": 8_388_608,
			"real_usage":  memUse,
			"si_disk":     0,
			"so_disk":     0,
			"swap_usage":  3,
			"total_real":  8_388_608,
			"total_swap":  2_097_152,
			"device":      "Memory",
		},
		"network": []map[string]any{
			{"device": "total", "rx": rx, "tx": tx},
			{"device": "eth0", "rx": rx / 2, "tx": tx / 2},
			{"device": "eth1", "rx": rx / 2, "tx": tx / 2},
		},
		"disk": map[string]any{
			"disk": []map[string]any{
				{"device": "sda", "display_name": "Drive 1", "read_access": 12, "write_access": 4, "read_byte": 4_800_000, "write_byte": 1_200_000, "util": diskUtil},
				{"device": "sdb", "display_name": "Drive 2", "read_access": 14, "write_access": 6, "read_byte": 5_100_000, "write_byte": 1_400_000, "util": diskUtil + 4},
				{"device": "sdc", "display_name": "Drive 3", "read_access": 8, "write_access": 2, "read_byte": 3_200_000, "write_byte": 800_000, "util": diskUtil - 4},
				{"device": "sdd", "display_name": "SSD Cache", "read_access": 22, "write_access": 18, "read_byte": 18_000_000, "write_byte": 7_500_000, "util": diskUtil + 12},
			},
			"total": map[string]any{
				"device": "total", "read_access": 56, "write_access": 30,
				"read_byte": 31_100_000, "write_byte": 10_900_000, "util": diskUtil + 6,
			},
		},
		"space": map[string]any{
			"total": map[string]any{"device": "total", "read_access": 56, "write_access": 30, "read_byte": 31_100_000, "write_byte": 10_900_000, "util": diskUtil + 6},
		},
		"time": time.Now().Unix(),
	}
}

// demoUtilSeries generates n per-slot samples in the modern "array of
// Utilization" response shape. Values follow the same alive-but-not-
// stressed bias as demoLiveUtilization, with a slow sinusoidal trend on
// top of the per-slot noise so the resulting sparkline reads like real
// activity rather than a flat random walk.
func (s *Server) demoUtilSeries(n int) []map[string]any {
	if n <= 0 {
		n = 1
	}
	now := time.Now().Unix()
	out := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		// Sinusoidal bias 0..1 oscillating ~3 cycles across the window
		// gives "calm → busy → calm" stripes that look alive in a chart.
		phase := float64(i) / float64(n)
		trend := 0.5 + 0.4*math.Sin(phase*2*math.Pi*3)

		cpuUser := int(8 + trend*10 + float64(s.intN(6)))
		cpuSys := int(4 + trend*4 + float64(s.intN(3)))
		cpuOther := 2 + s.intN(4)
		memUse := int(56 + trend*8 + float64(s.intN(3))) // %
		rx := int64(1_500_000 + int64(trend*9_000_000) + int64(s.intN(1_200_000)))
		tx := int64(300_000 + int64(trend*1_500_000) + int64(s.intN(400_000)))
		diskUtil := int(15 + trend*45 + float64(s.intN(8)))

		// Per-slot timestamp: oldest first, latest last.
		slotTime := now - int64(float64(n-1-i)*sliceSecondsForCount(n))

		out[i] = map[string]any{
			"cpu": map[string]any{
				"1min_load":   17,
				"5min_load":   22,
				"15min_load":  19,
				"user_load":   cpuUser,
				"system_load": cpuSys,
				"other_load":  cpuOther,
				"device":      "System",
			},
			"memory": map[string]any{
				"avail_real":  2_950_000,
				"avail_swap":  2_097_152,
				"buffer":      384_000,
				"cached":      1_584_000,
				"memory_size": 8_388_608,
				"real_usage":  memUse,
				"si_disk":     0,
				"so_disk":     0,
				"swap_usage":  3,
				"total_real":  8_388_608,
				"total_swap":  2_097_152,
				"device":      "Memory",
			},
			"network": []map[string]any{
				{"device": "total", "rx": rx, "tx": tx},
				{"device": "eth0", "rx": rx / 2, "tx": tx / 2},
				{"device": "eth1", "rx": rx / 2, "tx": tx / 2},
			},
			"disk": map[string]any{
				"total": map[string]any{
					"device": "total", "read_access": 56, "write_access": 30,
					"read_byte": 31_100_000, "write_byte": 10_900_000, "util": diskUtil,
				},
			},
			"space": map[string]any{
				"total": map[string]any{"device": "total", "util": diskUtil},
			},
			"time": slotTime,
		}
	}
	return out
}

// sliceSecondsForCount returns a plausible "seconds per slot" so the
// per-sample timestamps land in a sensible range. The Resource Monitor
// doesn't use these timestamps today (the axis is fixed-label), but real
// firmwares emit them and this keeps the demo payload faithful.
func sliceSecondsForCount(n int) float64 {
	switch {
	case n >= 84 && n <= 96:
		// Day window → 15-minute slots; week → ~2-hour slots.
		if n == 96 {
			return 15 * 60
		}
		return 2 * 3600
	case n == 60:
		// hour → 1-minute slots; month → 12-hour slots.
		// Caller distinguishes by length; we can't tell, so split the
		// difference (1 minute is fine — only used for the embedded
		// time field, which the view ignores).
		return 60
	case n == 52:
		return 7 * 86400 // year → 1-week slots
	}
	return 60
}

func (s *Server) handleProcessList(_ *Server, _ url.Values) any {
	return map[string]any{
		"process": demoProcesses,
	}
}

// — storage —

func (s *Server) handleStorage(_ *Server, _ url.Values) any {
	return demoStorage
}

// — shares —

func (s *Server) handleShares(_ *Server, _ url.Values) any {
	return map[string]any{
		"shares": demoShares,
		"total":  len(demoShares),
	}
}

func (s *Server) handleSnapshotsList(_ *Server, form url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	share := form.Get("name")
	snaps := s.data.snapshots[share]
	if snaps == nil {
		snaps = []snapshot{}
	}
	return map[string]any{"snapshots": snaps, "total": len(snaps)}
}

func (s *Server) handleSnapshotCreate(_ *Server, form url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	share := form.Get("name")
	desc := form.Get("desc")
	now := time.Now()
	name := "GMT+02-" + now.Format("2006.01.02-15.04.05")
	s.data.snapshots[share] = append(s.data.snapshots[share], snapshot{
		Name:        name,
		Time:        now.Unix(),
		Description: desc,
	})
	return map[string]any{}
}

func (s *Server) handleSnapshotDelete(_ *Server, form url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	share := form.Get("name")
	snaps := form.Get("snapshots")
	// snaps is JSON-encoded ["name"]; quick + dirty extraction
	var names []string
	_ = json.Unmarshal([]byte(snaps), &names)
	target := map[string]bool{}
	for _, n := range names {
		target[n] = true
	}
	cur := s.data.snapshots[share]
	out := cur[:0]
	for _, sn := range cur {
		if !target[sn.Name] {
			out = append(out, sn)
		}
	}
	s.data.snapshots[share] = out
	return map[string]any{}
}

// — file station —

func (s *Server) handleFileShares(_ *Server, _ url.Values) any {
	return map[string]any{
		"shares": demoFileShares,
		"total":  len(demoFileShares),
	}
}

func (s *Server) handleFileList(_ *Server, form url.Values) any {
	path := form.Get("folder_path")
	files, ok := demoFolderContents[path]
	if !ok {
		// Default — show a small "looks like nothing's here" listing.
		files = demoFolderContents["__default__"]
	}
	return map[string]any{"files": files, "total": len(files)}
}

func (s *Server) handleDirSizeStart(_ *Server, form url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	s.data.nextTaskID++
	tid := fmt.Sprintf("ds-%d", s.data.nextTaskID)
	// Pick a plausible total based on the path.
	path := trimWS(form.Get("path"))
	total, dirs, files := demoDirSize(path)
	s.data.dirSizeTasks[tid] = dirSizeTask{
		finished: false,
		at:       time.Now(),
		total:    total,
		numDirs:  dirs,
		numFiles: files,
	}
	return map[string]any{"taskid": tid}
}

func (s *Server) handleDirSizeStatus(_ *Server, form url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	tid := form.Get("taskid")
	t, ok := s.data.dirSizeTasks[tid]
	if !ok {
		return map[string]any{"finished": true, "total_size": 0, "num_dir": 0, "num_file": 0}
	}
	// Finish after 200ms of wall-clock so the "sizing…" placeholder is
	// briefly visible. Real DSM is slower; we want demos to feel snappy.
	if !t.finished && time.Since(t.at) > 200*time.Millisecond {
		t.finished = true
		s.data.dirSizeTasks[tid] = t
	}
	return map[string]any{
		"finished":   t.finished,
		"total_size": t.total,
		"num_dir":    t.numDirs,
		"num_file":   t.numFiles,
	}
}

// — users / network / logs —

func (s *Server) handleUsers(_ *Server, _ url.Values) any {
	return map[string]any{"users": demoUsers, "total": len(demoUsers), "offset": 0}
}

func (s *Server) handleGroups(_ *Server, _ url.Values) any {
	return map[string]any{"groups": demoGroups, "total": len(demoGroups), "offset": 0}
}

func (s *Server) handleNetwork(_ *Server, _ url.Values) any {
	return map[string]any{"interfaces": demoNetworkInterfaces}
}

func (s *Server) handleLogs(_ *Server, form url.Values) any {
	source := form.Get("logtype")
	if source == "" {
		source = "system"
	}
	items := demoLogs[source]
	offset, _ := strconv.Atoi(form.Get("start"))
	limit, _ := strconv.Atoi(form.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	if offset > len(items) {
		offset = len(items)
	}
	return map[string]any{"items": items[offset:end], "total": len(items)}
}

// — packages + services —

func (s *Server) handlePackages(_ *Server, _ url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	pkgs := make([]map[string]any, 0, len(demoPackages)+len(s.data.pkgExtra))
	seen := map[string]bool{}
	for _, p := range demoPackages {
		id := p["id"].(string)
		seen[id] = true
		pkgs = append(pkgs, s.demoPackageRow(p))
	}
	for id, p := range s.data.pkgExtra {
		if seen[id] {
			continue
		}
		pkgs = append(pkgs, s.demoPackageRow(p))
	}
	return map[string]any{"packages": pkgs, "total": len(pkgs)}
}

func (s *Server) handlePackageCatalog(_ *Server, _ url.Values) any {
	return map[string]any{"packages": demoCatalog, "total": len(demoCatalog)}
}

func (s *Server) handleInstallCheck(_ *Server, _ url.Values) any {
	return map[string]any{
		"is_occupied": false,
		"volume_list": []map[string]any{
			{"mount_point": "/volume1", "desc": "Volume 1"},
		},
	}
}

func (s *Server) handleInstallQueue(_ *Server, form url.Values) any {
	var req []struct {
		Pkg  string `json:"pkg"`
		Beta bool   `json:"beta"`
	}
	_ = json.Unmarshal([]byte(form.Get("pkgs")), &req)
	queue := make([]map[string]any, 0, len(req))
	for _, p := range req {
		if p.Pkg == "" {
			continue
		}
		queue = append(queue, map[string]any{"pkg": p.Pkg, "beta": p.Beta, "volume": ""})
	}
	return map[string]any{
		"queue":              queue,
		"broken_pkgs":        []string{},
		"cause_pausing_pkgs": []string{},
		"conflicted_pkgs":    []string{},
		"non_exist_pkgs":     []string{},
		"paused_pkgs":        []string{},
		"replaced_pkgs":      []string{},
	}
}

func (s *Server) handleInstallPackage(_ *Server, form url.Values) any {
	id := firstNonEmpty(form.Get("name"), form.Get("id"))
	if id == "" {
		return map[string]any{"progress": -1, "error": "missing package name"}
	}
	row := demoCatalogPackage(id)
	if row == nil {
		row = map[string]any{"id": id, "package": id, "name": id, "version": "", "maintainer": "Synology Inc.", "desc": ""}
	}
	installed := map[string]any{
		"id":            firstNonEmpty(stringValue(row, "id"), stringValue(row, "package")),
		"name":          firstNonEmpty(stringValue(row, "name"), id),
		"version":       stringValue(row, "version"),
		"maintainer":    stringValue(row, "maintainer"),
		"description":   firstNonEmpty(stringValue(row, "description"), stringValue(row, "desc")),
		"beta":          boolValue(row, "beta"),
		"ctl_uninstall": true,
		"timestamp":     time.Now().UnixMilli(),
	}
	s.data.mu.Lock()
	s.data.pkgExtra[id] = installed
	s.data.pkgState[id] = "running"
	s.data.mu.Unlock()
	return map[string]any{"progress": 1}
}

func (s *Server) handleInstallStart(_ *Server, _ url.Values) any {
	return map[string]any{"taskid": "install-demo"}
}

func (s *Server) handleInstallStatus(_ *Server, _ url.Values) any {
	return map[string]any{"finished": true, "stage": "done", "status": "success"}
}

func (s *Server) handlePkgControl(newState string) handlerFn {
	return func(s *Server, form url.Values) any {
		id := form.Get("id")
		s.data.mu.Lock()
		s.data.pkgState[id] = newState
		s.data.mu.Unlock()
		return map[string]any{}
	}
}

func (s *Server) demoPackageRow(p map[string]any) map[string]any {
	id := stringValue(p, "id")
	status := s.data.pkgState[id]
	if status == "" {
		status = "running"
	}
	c := map[string]any{}
	for k, v := range p {
		c[k] = v
	}
	c["additional"] = map[string]any{
		"status":        status,
		"maintainer":    p["maintainer"],
		"description":   firstNonEmpty(stringValue(p, "description"), stringValue(p, "desc")),
		"beta":          boolValue(p, "beta"),
		"ctl_uninstall": p["ctl_uninstall"],
	}
	return c
}

func demoCatalogPackage(id string) map[string]any {
	for _, p := range demoCatalog {
		if stringValue(p, "id") == id || stringValue(p, "package") == id {
			c := map[string]any{}
			for k, v := range p {
				c[k] = v
			}
			return c
		}
	}
	return nil
}

func stringValue(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolValue(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func (s *Server) handleServices(_ *Server, _ url.Values) any {
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	out := make([]map[string]any, 0, len(demoServices))
	for _, svc := range demoServices {
		c := map[string]any{}
		for k, v := range svc {
			c[k] = v
		}
		// Apply any state mutation from start/enable/disable.
		if st, ok := s.data.services[svc["service"].(string)]; ok {
			c["enable_status"] = st
		}
		out = append(out, c)
	}
	return map[string]any{"service": out, "total": len(out)}
}

// — containers —

func (s *Server) handleContainers(_ *Server, _ url.Values) any {
	return map[string]any{"containers": demoContainers, "total": len(demoContainers)}
}
func (s *Server) handleDockerImages(_ *Server, _ url.Values) any {
	return map[string]any{"images": demoDockerImages, "total": len(demoDockerImages)}
}
func (s *Server) handleDockerNetworks(_ *Server, _ url.Values) any {
	return map[string]any{"networks": demoDockerNetworks, "total": len(demoDockerNetworks)}
}

// — surveillance —

func (s *Server) handleCameras(_ *Server, _ url.Values) any {
	return map[string]any{"cameras": demoCameras, "total": len(demoCameras)}
}
func (s *Server) handleRecordings(_ *Server, _ url.Values) any {
	return map[string]any{"recordings": demoRecordings, "total": len(demoRecordings)}
}
func (s *Server) handleSurveillanceInfo(_ *Server, _ url.Values) any {
	return demoSurveillanceInfo
}

// — backup —

func (s *Server) handleHyperBackup(_ *Server, _ url.Values) any {
	return map[string]any{"task_list": demoHyperBackupTasks, "total": len(demoHyperBackupTasks)}
}
func (s *Server) handleActiveBackup(_ *Server, _ url.Values) any {
	return map[string]any{"task_list": demoActiveBackupTasks, "total": len(demoActiveBackupTasks)}
}
func (s *Server) handleActiveBackupVersions(_ *Server, _ url.Values) any {
	return map[string]any{"version_list": demoActiveBackupVersions, "total": len(demoActiveBackupVersions)}
}
func (s *Server) handleCloudSync(_ *Server, _ url.Values) any {
	return map[string]any{"connections": demoCloudSyncTasks, "total": len(demoCloudSyncTasks)}
}

// — drive —

func (s *Server) handleDriveFiles(_ *Server, _ url.Values) any {
	return map[string]any{"items": demoDriveFiles, "total": len(demoDriveFiles)}
}
func (s *Server) handleDriveStats(_ *Server, _ url.Values) any {
	return demoDriveStats
}

// — security —

func (s *Server) handleCerts(_ *Server, _ url.Values) any {
	return map[string]any{"certificates": demoCertificates, "total": len(demoCertificates)}
}
func (s *Server) handleSecAdvisorConf(_ *Server, _ url.Values) any {
	return demoSecAdvisorConf
}
func (s *Server) handleSecAdvisorItems(_ *Server, _ url.Values) any {
	return map[string]any{"items": demoSecAdvisorItems, "total": len(demoSecAdvisorItems)}
}
func (s *Server) handleSchedTasks(_ *Server, _ url.Values) any {
	return map[string]any{"tasks": demoSchedTasks, "total": len(demoSchedTasks)}
}
func (s *Server) handleFirewallStatus(_ *Server, _ url.Values) any {
	return demoFirewallStatus
}
func (s *Server) handleFirewallProfiles(_ *Server, _ url.Values) any {
	return map[string]any{"profiles": demoFirewallProfiles, "total": len(demoFirewallProfiles)}
}
func (s *Server) handleFirewallRules(_ *Server, _ url.Values) any {
	return map[string]any{"rules": demoFirewallRules, "total": len(demoFirewallRules)}
}
func (s *Server) handleDDNSProviders(_ *Server, _ url.Values) any {
	return map[string]any{"providers": demoDDNSProviders, "total": len(demoDDNSProviders)}
}
func (s *Server) handleDDNSRecords(_ *Server, _ url.Values) any {
	return map[string]any{"records": demoDDNSRecords, "total": len(demoDDNSRecords)}
}
func (s *Server) handleDDNSExtIP(_ *Server, _ url.Values) any {
	return map[string]any{"ipv4": "203.0.113.42", "ipv6": "2001:db8::42"}
}

// — notifications —

func (s *Server) handleNotificationSettings(_ *Server, _ url.Values) any {
	return demoNotificationSettings
}

func (s *Server) handleNotificationLog(_ *Server, _ url.Values) any {
	return map[string]any{"logs": demoNotificationLog, "total": len(demoNotificationLog)}
}

// — quotas —

func (s *Server) handleUserQuotas(_ *Server, _ url.Values) any {
	return map[string]any{"users": demoUserQuotas, "total": len(demoUserQuotas)}
}

// — virtual machine manager —

func (s *Server) handleVMs(_ *Server, _ url.Values) any {
	return map[string]any{"guests": demoVMs, "total": len(demoVMs)}
}
func (s *Server) handleVMHosts(_ *Server, _ url.Values) any {
	return map[string]any{"hosts": demoVMHosts, "total": len(demoVMHosts)}
}

// — iscsi / san manager —

func (s *Server) handleISCSITargets(_ *Server, _ url.Values) any {
	return map[string]any{"targets": demoISCSITargets, "total": len(demoISCSITargets)}
}
func (s *Server) handleISCSILUNs(_ *Server, _ url.Values) any {
	return map[string]any{"luns": demoISCSILUNs, "total": len(demoISCSILUNs)}
}

// — settings: DSM update / time-region / power / external-access —

// handleDSMUpdate serves SYNO.Core.Upgrade.Server `check`. The fields
// closely mirror what real DSM 7.2 returns; the dsm package walks
// both nested `current.version` / `update.version` keys and the
// flat metadata.
func (s *Server) handleDSMUpdate(_ *Server, _ url.Values) any {
	return demoDSMUpdate
}

// handleDSMDownloadStatus serves SYNO.Core.Upgrade `download_status`.
// The real endpoint reports what's queued for install; we surface
// the same "update available" envelope as the catalog call so the
// dsm package's fallback path produces a consistent answer.
func (s *Server) handleDSMDownloadStatus(_ *Server, _ url.Values) any {
	return map[string]any{
		"version": "DSM 7.2.2-72806 Update 3",
		"status":  "idle",
		"update": map[string]any{
			"available": true,
			"version":   "DSM 7.2.2-72806 Update 4",
		},
	}
}

func (s *Server) handleRegionNTP(_ *Server, _ url.Values) any      { return demoTimeRegionNTP }
func (s *Server) handleRegionLanguage(_ *Server, _ url.Values) any { return demoTimeRegionLang }

func (s *Server) handlePowerSchedule(_ *Server, _ url.Values) any {
	return map[string]any{"schedules": demoPowerSchedule, "total": len(demoPowerSchedule)}
}

func (s *Server) handleWakeOnLAN(_ *Server, _ url.Values) any { return demoWakeOnLAN }

func (s *Server) handleQuickConnect(_ *Server, _ url.Values) any { return demoQuickConnect }

func (s *Server) handlePortForwarding(_ *Server, _ url.Values) any {
	return demoPortForwarding
}
