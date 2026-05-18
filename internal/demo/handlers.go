package demo

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
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
		"account":   "baraniewski",
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

func (s *Server) handleUtilization(_ *Server, _ url.Values) any {
	// Randomize within a plausible band so sparklines move during a
	// demo session. The base values are chosen so screenshots look
	// "alive but not stressed".
	cpuUser := 8 + rand.IntN(12)
	cpuSys := 4 + rand.IntN(6)
	cpuOther := 2 + rand.IntN(4)
	memUse := 58 + rand.IntN(8) // %
	rx := int64(2_500_000 + rand.IntN(8_000_000))
	tx := int64(400_000 + rand.IntN(1_200_000))
	diskUtil := 18 + rand.IntN(40)
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
	pkgs := make([]map[string]any, 0, len(demoPackages))
	for _, p := range demoPackages {
		status := s.data.pkgState[p["id"].(string)]
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
			"description":   p["description"],
			"beta":          p["beta"] == true,
			"ctl_uninstall": p["ctl_uninstall"],
		}
		pkgs = append(pkgs, c)
	}
	return map[string]any{"packages": pkgs, "total": len(pkgs)}
}

func (s *Server) handlePackageCatalog(_ *Server, _ url.Values) any {
	return map[string]any{"packages": demoCatalog, "total": len(demoCatalog)}
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
