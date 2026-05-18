package demo

// Canned demo data. Everything here is hand-tuned to look like a
// real, lived-in homelab NAS:
//   - 5+ month uptime, mixed-state packages, a couple of issues to
//     show off the alert colouring
//   - File trees deep enough to demo drill-down + dir-sizing
//   - Realistic share names, container stack, surveillance setup
//
// Shape: every var here maps 1:1 to a Synology DSM JSON envelope's
// `data` payload. We deliberately use map[string]any / []map[string]any
// instead of the typed dsm structs to avoid an import cycle and so the
// demo can advertise fields the typed structs don't model yet.

import (
	"strings"
	"time"
)

// — seeds for mutable state (server.newState() copies these) —

var demoServicesSeed = map[string]string{
	"nfs-server": "enabled",
	"smb":        "enabled",
	"ssh-shell":  "enabled",
	"snmpd":      "always-on",
	"ntpd":       "enabled",
	"telnetd":    "disabled",
	"ftp-pure":   "disabled",
	"sftp":       "disabled",
	"bonjour":    "disabled",
	"webdav":     "enabled",
	"upnp":       "enabled",
	"rsyncd":     "disabled",
	"tftp":       "disabled",
	"cupsd":      "always-on",
	"synoscgi":   "always-on",
	"pkg-iscsi":  "always-on",
	"ups-net":    "disabled",
	"ups-usb":    "always-on",
}

var demoPackagesStateSeed = map[string]string{
	// Force-stop a couple of installed packages so screenshots show
	// the stop chip alongside running.
	"NoteStation":     "stop",
	"DownloadStation": "running",
}

// — top processes —

// Real DSM boxes run 100+ processes. Hand-picked here to be representative
// — kernel threads, syno-* daemons, samba stack, NFS stack, containerd-shim
// for every running container, package services, media servers, etc.
// CPU values are roughly weighted so a few stand out and the rest cluster
// at low single digits, matching what `top` looks like on a healthy NAS.
var demoProcesses = []map[string]any{
	// Big CPU
	{"pid": 11242, "command": "plex_media_server", "cpu": 18, "mem": 412384, "status": "S"},
	{"pid": 1182, "command": "synoindexd", "cpu": 9, "mem": 88412, "status": "S"},
	{"pid": 2310, "command": "syno-photos-indexer", "cpu": 7, "mem": 156388, "status": "S"},
	{"pid": 5421, "command": "containerd-shim-runc-v2 plex", "cpu": 5, "mem": 64210, "status": "S"},
	{"pid": 5430, "command": "containerd-shim-runc-v2 jellyfin", "cpu": 5, "mem": 52140, "status": "S"},
	{"pid": 982, "command": "smbd", "cpu": 4, "mem": 41280, "status": "S"},
	{"pid": 8721, "command": "synology-drive-server", "cpu": 3, "mem": 198432, "status": "S"},
	{"pid": 11242, "command": "ffmpeg-transcoder", "cpu": 12, "mem": 224380, "status": "R"},
	{"pid": 9988, "command": "synoaicore", "cpu": 6, "mem": 412380, "status": "S"},
	// Container shims
	{"pid": 5444, "command": "containerd-shim-runc-v2 homepage", "cpu": 1, "mem": 12480, "status": "S"},
	{"pid": 5455, "command": "containerd-shim-runc-v2 traefik", "cpu": 2, "mem": 18120, "status": "S"},
	{"pid": 5466, "command": "containerd-shim-runc-v2 uptime-kuma", "cpu": 1, "mem": 22480, "status": "S"},
	{"pid": 4200, "command": "containerd", "cpu": 3, "mem": 88240, "status": "S"},
	{"pid": 4214, "command": "dockerd", "cpu": 2, "mem": 128400, "status": "S"},
	// Syno-* daemons
	{"pid": 14411, "command": "tailscaled", "cpu": 2, "mem": 35200, "status": "S"},
	{"pid": 7711, "command": "synoavd", "cpu": 2, "mem": 102400, "status": "S"},
	{"pid": 11234, "command": "active-backup-bus", "cpu": 1, "mem": 67400, "status": "S"},
	{"pid": 622, "command": "synologin", "cpu": 1, "mem": 22150, "status": "S"},
	{"pid": 711, "command": "synoschedd", "cpu": 1, "mem": 19400, "status": "S"},
	{"pid": 712, "command": "synobasic", "cpu": 1, "mem": 24800, "status": "S"},
	{"pid": 713, "command": "synocachefsd", "cpu": 0, "mem": 42100, "status": "S"},
	{"pid": 714, "command": "synouser", "cpu": 0, "mem": 11420, "status": "S"},
	{"pid": 715, "command": "synoupgrade", "cpu": 0, "mem": 9240, "status": "S"},
	{"pid": 716, "command": "synocgrpd", "cpu": 0, "mem": 11400, "status": "S"},
	{"pid": 717, "command": "synoschdcfgd", "cpu": 0, "mem": 8200, "status": "S"},
	{"pid": 718, "command": "synohddmond", "cpu": 0, "mem": 12400, "status": "S"},
	{"pid": 719, "command": "synologmgr", "cpu": 1, "mem": 18800, "status": "S"},
	{"pid": 720, "command": "synonetd", "cpu": 0, "mem": 14200, "status": "S"},
	{"pid": 721, "command": "synomountd", "cpu": 0, "mem": 9800, "status": "S"},
	{"pid": 722, "command": "synonotifyd", "cpu": 0, "mem": 16400, "status": "S"},
	{"pid": 723, "command": "synopkgctl", "cpu": 0, "mem": 12400, "status": "S"},
	{"pid": 724, "command": "synoschedtask", "cpu": 0, "mem": 15400, "status": "S"},
	{"pid": 725, "command": "synosharing", "cpu": 0, "mem": 12200, "status": "S"},
	{"pid": 726, "command": "synosnmpcd", "cpu": 0, "mem": 9400, "status": "S"},
	{"pid": 727, "command": "synosurfshare", "cpu": 0, "mem": 14400, "status": "S"},
	{"pid": 728, "command": "synotifyd", "cpu": 0, "mem": 11200, "status": "S"},
	{"pid": 729, "command": "synowebapi", "cpu": 1, "mem": 28400, "status": "S"},
	{"pid": 730, "command": "synowebcgid", "cpu": 0, "mem": 14400, "status": "S"},
	// Samba / NFS stack
	{"pid": 983, "command": "nmbd", "cpu": 0, "mem": 18420, "status": "S"},
	{"pid": 984, "command": "winbindd", "cpu": 0, "mem": 22480, "status": "S"},
	{"pid": 1101, "command": "nfsd", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 1102, "command": "rpc.mountd", "cpu": 0, "mem": 4200, "status": "S"},
	{"pid": 1103, "command": "rpc.statd", "cpu": 0, "mem": 3800, "status": "S"},
	{"pid": 1104, "command": "rpc.idmapd", "cpu": 0, "mem": 4400, "status": "S"},
	{"pid": 1105, "command": "rpcbind", "cpu": 0, "mem": 3200, "status": "S"},
	// Network / daemons
	{"pid": 312, "command": "sshd", "cpu": 0, "mem": 6800, "status": "S"},
	{"pid": 322, "command": "cupsd", "cpu": 0, "mem": 8420, "status": "S"},
	{"pid": 412, "command": "ntpd", "cpu": 0, "mem": 4800, "status": "S"},
	{"pid": 422, "command": "snmpd", "cpu": 0, "mem": 14820, "status": "S"},
	{"pid": 512, "command": "avahi-daemon", "cpu": 0, "mem": 4400, "status": "S"},
	{"pid": 522, "command": "crond", "cpu": 0, "mem": 3200, "status": "S"},
	{"pid": 532, "command": "syslog-ng", "cpu": 0, "mem": 28400, "status": "S"},
	{"pid": 612, "command": "nginx: master process", "cpu": 0, "mem": 12400, "status": "S"},
	{"pid": 613, "command": "nginx: worker", "cpu": 0, "mem": 18400, "status": "S"},
	{"pid": 614, "command": "nginx: worker", "cpu": 0, "mem": 18200, "status": "S"},
	{"pid": 615, "command": "php-fpm: master", "cpu": 0, "mem": 22400, "status": "S"},
	{"pid": 616, "command": "php-fpm: pool www", "cpu": 0, "mem": 38400, "status": "S"},
	{"pid": 617, "command": "php-fpm: pool www", "cpu": 0, "mem": 36800, "status": "S"},
	// Synology Photos / Drive
	{"pid": 2400, "command": "synofotoindexer", "cpu": 3, "mem": 82400, "status": "S"},
	{"pid": 2410, "command": "synofotoanalyzer", "cpu": 2, "mem": 124800, "status": "S"},
	{"pid": 2420, "command": "synofotoencoder", "cpu": 1, "mem": 38400, "status": "S"},
	{"pid": 2430, "command": "synocloudsyncd", "cpu": 0, "mem": 42800, "status": "S"},
	// Backup
	{"pid": 6101, "command": "synobackupd", "cpu": 0, "mem": 32400, "status": "S"},
	{"pid": 6102, "command": "synohyperbackup", "cpu": 1, "mem": 48400, "status": "S"},
	{"pid": 6103, "command": "active-backup-vbus", "cpu": 0, "mem": 28400, "status": "S"},
	// Container apps (inside-container processes show up too)
	{"pid": 11243, "command": "plex_transcoder", "cpu": 8, "mem": 184400, "status": "S"},
	{"pid": 11244, "command": "plex_analyzer", "cpu": 4, "mem": 64200, "status": "S"},
	{"pid": 11245, "command": "plex_dlna_server", "cpu": 0, "mem": 18400, "status": "S"},
	{"pid": 12001, "command": "jellyfin-server", "cpu": 3, "mem": 198400, "status": "S"},
	{"pid": 13001, "command": "traefik", "cpu": 2, "mem": 64200, "status": "S"},
	// Kernel threads (varied)
	{"pid": 2, "command": "[kthreadd]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 3, "command": "[rcu_gp]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 4, "command": "[rcu_par_gp]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 10, "command": "[migration/0]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 11, "command": "[ksoftirqd/0]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 12, "command": "[migration/1]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 13, "command": "[ksoftirqd/1]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 20, "command": "[kworker/0:0-events]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 21, "command": "[kworker/0:1-mm_percpu_wq]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 22, "command": "[kworker/1:0-events]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 23, "command": "[kworker/1:1H]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 31, "command": "[kswapd0]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 32, "command": "[ksmd]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 41, "command": "[scsi_eh_0]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 42, "command": "[scsi_eh_1]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 43, "command": "[scsi_eh_2]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 44, "command": "[scsi_eh_3]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 51, "command": "[md0_raid1]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 52, "command": "[md1_raid5]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 53, "command": "[btrfs-transaction]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 54, "command": "[btrfs-cleaner]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 55, "command": "[btrfs-endio-meta]", "cpu": 0, "mem": 0, "status": "S"},
	{"pid": 56, "command": "[ext4-rsv-conver]", "cpu": 0, "mem": 0, "status": "S"},
}

// — storage —

var demoStorage = map[string]any{
	"volumes": []map[string]any{
		{
			"id": "volume_1", "vol_path": "/volume1", "container": "internal",
			"device_type": "shr_without_disk_protect", "desc": "SHR-1", "fs_type": "btrfs",
			"num_id": 1, "pool_path": "/volume1", "raidType": "shr1",
			"space_path": "/volume1", "is_writable": true,
			"size": map[string]any{
				"free_inode": "892834", "total_inode": "67108864",
				"total": "17592186044416", "used": "10031886745600",
			},
			"status": "normal", "summary_status": "normal",
		},
		{
			"id": "volume_2", "vol_path": "/volume2", "container": "internal",
			"device_type": "basic_with_data_protect", "desc": "Basic", "fs_type": "ext4",
			"num_id": 2, "pool_path": "/volume2", "raidType": "basic",
			"is_writable": true,
			"size": map[string]any{
				"free_inode": "3284532", "total_inode": "33554432",
				"total": "4398046511104", "used": "327680000000",
			},
			"status": "normal", "summary_status": "normal",
		},
	},
	"storagePools": []map[string]any{
		{"id": "pool_1", "device_type": "shr_without_disk_protect", "raidType": "shr1",
			"num_id": 1, "disks": []string{"/dev/sda", "/dev/sdb", "/dev/sdc"},
			"pool":   map[string]any{"status": "normal"},
			"size":   map[string]any{"total": "17592186044416", "used": "10031886745600"},
			"status": "normal", "summary_status": "normal"},
		{"id": "pool_2", "device_type": "basic_with_data_protect", "raidType": "basic",
			"num_id": 2, "disks": []string{"/dev/sdd"},
			"pool":   map[string]any{"status": "normal"},
			"size":   map[string]any{"total": "4398046511104", "used": "327680000000"},
			"status": "normal", "summary_status": "normal"},
	},
	"disks": []map[string]any{
		{"id": "/dev/sda", "path": "/dev/sda", "device": "sda", "diskType": "SATA",
			"model": "WD80EFAX-68LHPN0", "vendor": "WDC", "firm": "83.H0A83",
			"status": "normal", "temp": 39, "capacity": "8001563222016",
			"container": map[string]any{"order": 0, "type": "internal", "str": "Pool 1"},
			"smart":     map[string]any{"status": "ok"}, "num_id": 1, "serial": "VLG7DEMO1"},
		{"id": "/dev/sdb", "path": "/dev/sdb", "device": "sdb", "diskType": "SATA",
			"model": "WD80EFAX-68LHPN0", "vendor": "WDC", "firm": "83.H0A83",
			"status": "normal", "temp": 41, "capacity": "8001563222016",
			"container": map[string]any{"order": 1, "type": "internal", "str": "Pool 1"},
			"smart":     map[string]any{"status": "ok"}, "num_id": 2, "serial": "VLG7DEMO2"},
		{"id": "/dev/sdc", "path": "/dev/sdc", "device": "sdc", "diskType": "SATA",
			"model": "ST4000VN006-3CW104", "vendor": "Seagate", "firm": "SC60",
			"status": "normal", "temp": 36, "capacity": "4000787030016",
			"container": map[string]any{"order": 2, "type": "internal", "str": "Pool 1"},
			"smart":     map[string]any{"status": "warn"}, "num_id": 3, "serial": "ZGY8DEMO3"},
		{"id": "/dev/sdd", "path": "/dev/sdd", "device": "sdd", "diskType": "SSD",
			"model": "Samsung SSD 870 EVO 1TB", "vendor": "Samsung", "firm": "SVT02B6Q",
			"status": "normal", "temp": 31, "capacity": "1024209543168",
			"container": map[string]any{"order": 3, "type": "internal", "str": "Pool 2"},
			"smart":     map[string]any{"status": "ok"}, "num_id": 4, "serial": "S6DEMODISK4"},
	},
}

// — shares —

var demoShares = []map[string]any{
	{"name": "photo", "vol_path": "/volume1", "desc": "Family photos + auto-backup target", "encryption": 1, "enc_status": 1, "hidden": false, "enable_recycle_bin": true, "is_readonly": false, "is_usb_share": false, "is_sync_share": false, "is_cloudsync_share": false, "share_quota": 2_000_000, "share_quota_used": 1_265_404},
	{"name": "video", "vol_path": "/volume1", "desc": "Movies + TV", "hidden": false, "enable_recycle_bin": true, "share_quota": 0, "share_quota_used": 0},
	{"name": "music", "vol_path": "/volume1", "desc": "FLAC + MP3 library", "hidden": false, "enable_recycle_bin": false},
	{"name": "books", "vol_path": "/volume1", "desc": "ePub / PDF / audiobooks", "hidden": false, "enable_recycle_bin": false},
	{"name": "homes", "vol_path": "/volume1", "desc": "Per-user home folders", "hidden": true, "enable_recycle_bin": true},
	{"name": "backups", "vol_path": "/volume2", "desc": "Hyper Backup destination", "hidden": false, "enable_recycle_bin": false, "is_readonly": false},
	{"name": "plex-cache", "vol_path": "/volume2", "desc": "Plex transcode + metadata", "hidden": false, "is_usb_share": false},
	{"name": "code", "vol_path": "/volume1", "desc": "Synology Drive sync target", "hidden": false, "is_sync_share": true},
	{"name": "surveillance", "vol_path": "/volume2", "desc": "Surveillance Station recordings", "hidden": false, "is_readonly": false},
	{"name": "cloud-sync", "vol_path": "/volume1", "desc": "Cloud Sync (Dropbox + Drive)", "hidden": false, "is_cloudsync_share": true},
}

// — file station roots (per-share view) —

var demoFileShares = []map[string]any{
	{"isdir": true, "name": "photo", "path": "/photo",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
	{"isdir": true, "name": "video", "path": "/video",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
	{"isdir": true, "name": "music", "path": "/music",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
	{"isdir": true, "name": "books", "path": "/books",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
	{"isdir": true, "name": "homes", "path": "/homes",
		"additional": map[string]any{"owner": map[string]any{"user": "root", "group": "root"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
	{"isdir": true, "name": "backups", "path": "/backups",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 4_070_366_511_104, "totalspace": 4_398_046_511_104, "readonly": false}}},
	{"isdir": true, "name": "code", "path": "/code",
		"additional": map[string]any{"owner": map[string]any{"user": "baraniewski", "group": "users"},
			"volume_status": map[string]any{"freespace": 7_560_299_298_816, "totalspace": 17_592_186_044_416, "readonly": false}}},
}

// — folder contents (keyed by path) —

func fsdir(name, path string) map[string]any {
	return map[string]any{"isdir": true, "name": name, "path": path, "additional": map[string]any{
		"owner": map[string]any{"user": "baraniewski", "group": "users"},
		"time":  map[string]any{"mtime": time.Now().Add(-72 * time.Hour).Unix()},
		"perm":  map[string]any{"posix": 755},
	}}
}
func fsfile(name, path string, size int64, modAgo time.Duration) map[string]any {
	return map[string]any{"isdir": false, "name": name, "path": path, "additional": map[string]any{
		"owner": map[string]any{"user": "baraniewski", "group": "users"},
		"size":  size,
		"time":  map[string]any{"mtime": time.Now().Add(-modAgo).Unix()},
		"perm":  map[string]any{"posix": 644},
	}}
}

var demoFolderContents = map[string][]map[string]any{
	"/photo": {
		fsdir("2026", "/photo/2026"),
		fsdir("2025", "/photo/2025"),
		fsdir("2024", "/photo/2024"),
		fsdir("Family", "/photo/Family"),
		fsdir("Travel", "/photo/Travel"),
		fsdir("Wedding", "/photo/Wedding"),
		fsfile("front.jpg", "/photo/front.jpg", 1_034_240, 8*time.Hour),
		fsfile("DSC02184.RAF", "/photo/DSC02184.RAF", 38_421_400, 28*time.Hour),
		fsfile("DSC02185.RAF", "/photo/DSC02185.RAF", 41_388_812, 28*time.Hour),
	},
	"/photo/2026": {
		fsdir("01-jan", "/photo/2026/01-jan"),
		fsdir("02-feb", "/photo/2026/02-feb"),
		fsdir("03-mar", "/photo/2026/03-mar"),
		fsdir("04-apr", "/photo/2026/04-apr"),
		fsdir("05-may", "/photo/2026/05-may"),
	},
	"/video": {
		fsdir("Movies", "/video/Movies"),
		fsdir("TV", "/video/TV"),
		fsdir("Home", "/video/Home"),
		fsfile("rip-2026-03-bday.mp4", "/video/rip-2026-03-bday.mp4", 4_280_412_192, 30*24*time.Hour),
	},
	"/music": {
		fsdir("FLAC", "/music/FLAC"),
		fsdir("Lossy", "/music/Lossy"),
		fsdir("Playlists", "/music/Playlists"),
	},
	"/books": {
		fsdir("fiction", "/books/fiction"),
		fsdir("non-fiction", "/books/non-fiction"),
		fsdir("audiobooks", "/books/audiobooks"),
		fsfile("how-linux-works-3rd.pdf", "/books/how-linux-works-3rd.pdf", 14_280_412, 10*24*time.Hour),
		fsfile("the-pragmatic-programmer.epub", "/books/the-pragmatic-programmer.epub", 2_140_220, 80*24*time.Hour),
	},
	"/homes": {
		fsdir("baraniewski", "/homes/baraniewski"),
		fsdir("demo", "/homes/demo"),
	},
	"/backups": {
		fsdir("hyperbackup", "/backups/hyperbackup"),
		fsdir("active-backup", "/backups/active-backup"),
		fsdir("offsite-cold", "/backups/offsite-cold"),
	},
	"/code": {
		fsdir("synology-ctl", "/code/synology-ctl"),
		fsdir("openusage", "/code/openusage"),
		fsdir("homelab-iac", "/code/homelab-iac"),
		fsfile(".gitconfig", "/code/.gitconfig", 1248, 200*24*time.Hour),
	},
	"__default__": {
		fsdir(".cache", "/__default__/.cache"),
		fsfile("README.md", "/__default__/README.md", 4280, 24*time.Hour),
	},
}

// demoDirSize returns plausible total/dirs/files for a given path.
func demoDirSize(path string) (int64, int64, int64) {
	// Quote-aware: dsm.Client sends path as `["..."]`.
	path = strings.TrimPrefix(strings.TrimSuffix(path, `"]`), `["`)
	switch path {
	case "/photo":
		return 1_265_404_887_040, 12, 18432
	case "/video":
		return 6_482_440_142_848, 8, 1842
	case "/music":
		return 298_412_991_456, 6, 4288
	case "/books":
		return 18_240_412_416, 4, 1244
	case "/homes":
		return 232_140_412_416, 12, 24288
	case "/backups":
		return 980_280_412_416, 5, 188
	case "/code":
		return 12_840_412_416, 124, 18244
	case "/cloud-sync":
		return 480_412_416, 8, 488
	case "/surveillance":
		return 1_240_412_412_416, 8, 12440
	case "/plex-cache":
		return 76_240_412_416, 2, 4288
	}
	// Subfolder default — scale down a bit.
	return 1_280_412_416, 4, 88
}

// — users / groups —

var demoUsers = []map[string]any{
	{"name": "baraniewski", "uid": 1026, "description": "Jan (admin)", "email": "jan@example.com", "expired": "normal", "groups": []string{"administrators", "users"}, "password_never_expire": true},
	{"name": "demo", "uid": 1027, "description": "Demo viewer", "email": "demo@example.com", "expired": "normal", "groups": []string{"users"}},
	{"name": "backup-svc", "uid": 1028, "description": "Service account for Hyper Backup", "email": "", "expired": "normal", "groups": []string{"backup-operators"}, "password_never_expire": true},
	{"name": "guest", "uid": 1029, "description": "Disabled guest account", "email": "", "expired": "now", "groups": []string{"users"}},
	{"name": "kid", "uid": 1030, "description": "Family — restricted access", "email": "", "expired": "normal", "groups": []string{"users", "kids"}},
}

var demoGroups = []map[string]any{
	{"name": "administrators", "gid": 101, "description": "DSM admins"},
	{"name": "users", "gid": 100, "description": "Standard users"},
	{"name": "kids", "gid": 1050, "description": "Restricted profile"},
	{"name": "backup-operators", "gid": 1100, "description": "Allowed to write to /backups"},
	{"name": "developers", "gid": 1200, "description": "Synology Drive shared workspace"},
}

// — network —

var demoNetworkInterfaces = []map[string]any{
	{"id": "eth0", "ifname": "LAN 1", "type": "lan", "ip": "192.168.1.36", "mask": "255.255.255.0", "gateway": "192.168.1.1", "mac": "00:11:32:DE:M0:01", "mtu": 1500, "speed": 2500, "status": "connected", "use_dhcp": true},
	{"id": "eth1", "ifname": "LAN 2", "type": "lan", "ip": "10.0.0.36", "mask": "255.255.255.0", "gateway": "10.0.0.1", "mac": "00:11:32:DE:M0:02", "mtu": 9000, "speed": 1000, "status": "connected", "use_dhcp": false},
	{"id": "ovs_bond0", "ifname": "Bond 1", "type": "bond", "ip": "192.168.1.37", "mask": "255.255.255.0", "mac": "00:11:32:DE:M0:03", "mtu": 1500, "speed": 4000, "status": "connected", "use_dhcp": false},
	{"id": "tailscale0", "ifname": "Tailscale", "type": "vpn", "ip": "100.123.191.95", "mask": "255.255.255.255", "gateway": "100.100.100.100", "mac": "", "mtu": 1280, "speed": 0, "status": "connected", "use_dhcp": false},
}

// — logs —

func logEntry(t time.Time, level, user, ip, event, descr string) map[string]any {
	return map[string]any{
		"time":  t.Format("2006-01-02 15:04:05"),
		"level": level, "user": user, "ip": ip, "event": event, "descr": descr,
	}
}

var demoLogs = map[string][]map[string]any{
	"system": {
		logEntry(time.Now().Add(-15*time.Minute), "info", "System", "", "Volume 1 scrub completed", "0 errors detected; checksum matches across 6.4 TB of data."),
		logEntry(time.Now().Add(-32*time.Minute), "warn", "System", "", "Disk SMART warning", "sdc reported 4 reallocated sectors (was 3). Consider replacement."),
		logEntry(time.Now().Add(-1*time.Hour), "info", "Package", "", "Synology Drive started", "Service started after auto-update to 3.1.0-22920."),
		logEntry(time.Now().Add(-90*time.Minute), "info", "Hyper Backup", "", "Backup task completed", `"Daily homes → /volume2/backups" finished in 22m 14s.`),
		logEntry(time.Now().Add(-2*time.Hour), "info", "baraniewski", "192.168.1.5", "User logged in", "Login via DSM web UI from 192.168.1.5."),
		logEntry(time.Now().Add(-3*time.Hour), "warn", "Network", "", "Tailscale reconnected", "Tailscale lost connection at 17:02; auto-reconnected at 17:03."),
		logEntry(time.Now().Add(-4*time.Hour), "info", "System", "", "Scheduled task triggered", `"Antivirus weekly scan" started.`),
		logEntry(time.Now().Add(-5*time.Hour), "err", "Surveillance", "", "Camera disconnected", "Front Door camera unreachable for 3 minutes — recordings paused."),
		logEntry(time.Now().Add(-6*time.Hour), "info", "demo", "10.0.0.42", "User logged in", "Login via SSH."),
		logEntry(time.Now().Add(-7*time.Hour), "info", "Package", "", "Container Manager updated", "Updated to 24.0.2-1535."),
		logEntry(time.Now().Add(-9*time.Hour), "warn", "Security", "", "SecurityAdvisor flagged item", "SSH allowed from any IP — consider restricting to LAN."),
		logEntry(time.Now().Add(-10*time.Hour), "info", "Storage", "", "Snapshot created", `Snapshot of "photo" share created (daily auto).`),
		logEntry(time.Now().Add(-11*time.Hour), "info", "System", "", "DSM auto-update check", "No updates available."),
		logEntry(time.Now().Add(-13*time.Hour), "info", "Hyper Backup", "", "Backup task completed", `"Monthly cold storage → external" finished in 4h 38m.`),
		logEntry(time.Now().Add(-14*time.Hour), "err", "guest", "203.0.113.42", "Login blocked", "5 failed login attempts → IP auto-banned for 24h."),
		logEntry(time.Now().Add(-18*time.Hour), "info", "System", "", "NTP sync", "Time synchronised with pool.ntp.org (offset -0.142s)."),
		logEntry(time.Now().Add(-22*time.Hour), "info", "Package", "", "Active Backup ran", "laptop-jan ran scheduled backup (incremental, 412 MB)."),
		logEntry(time.Now().Add(-26*time.Hour), "warn", "Storage", "", "Volume nearly full", "Volume 1 used 91% — consider clean-up or expansion."),
		logEntry(time.Now().Add(-30*time.Hour), "info", "System", "", "Daily housekeeping", "Recycle bin auto-purge removed 38 items (1.4 GB)."),
	},
	"connection": {
		logEntry(time.Now().Add(-1*time.Hour), "info", "baraniewski", "192.168.1.5", "SMB connected", `Mounted "homes" via SMB.`),
		logEntry(time.Now().Add(-2*time.Hour), "info", "baraniewski", "100.123.191.95", "WebDAV connected", `Accessed "/code" via WebDAV.`),
		logEntry(time.Now().Add(-3*time.Hour), "info", "demo", "10.0.0.42", "SFTP connected", "SFTP session opened."),
		logEntry(time.Now().Add(-4*time.Hour), "info", "baraniewski", "192.168.1.5", "AFP connected", `Mounted "video" via AFP.`),
		logEntry(time.Now().Add(-6*time.Hour), "info", "backup-svc", "192.168.1.10", "rsync connected", "rsyncd session opened from 192.168.1.10."),
	},
}

// — packages —

var demoPackages = []map[string]any{
	{"id": "ActiveInsight", "name": "Active Insight", "version": "1.2.0-1214", "maintainer": "Synology Inc.", "description": "Cloud monitoring service for Synology NAS.", "beta": false, "ctl_uninstall": true, "timestamp": time.Now().Add(-45 * 24 * time.Hour).UnixMilli()},
	{"id": "AudioStation", "name": "Audio Station", "version": "7.0.3-5401", "maintainer": "Synology Inc.", "description": "Stream your audio collection from your NAS.", "beta": false, "ctl_uninstall": true},
	{"id": "CodecPack", "name": "Advanced Media Extensions", "version": "1.1.2-0301", "maintainer": "Synology Inc.", "description": "Codecs for advanced media playback.", "beta": false, "ctl_uninstall": true},
	{"id": "Container-Manager", "name": "Container Manager", "version": "24.0.2-1535", "maintainer": "Synology Inc.", "description": "Manage Docker containers and Compose stacks.", "beta": false, "ctl_uninstall": true},
	{"id": "DhcpServer", "name": "DHCP Server", "version": "1.0.1-0036", "maintainer": "Synology Inc.", "description": "Run a DHCP server on your NAS.", "beta": false, "ctl_uninstall": true},
	{"id": "DownloadStation", "name": "Download Station", "version": "3.9.5-4627", "maintainer": "Synology Inc.", "description": "BitTorrent + HTTP/FTP download manager.", "beta": false, "ctl_uninstall": true},
	{"id": "FileStation", "name": "File Station", "version": "1.3.1-1382", "maintainer": "Synology Inc.", "description": "Web-based file manager (always installed).", "beta": false, "ctl_uninstall": false},
	{"id": "Git", "name": "Git Server", "version": "2.39.1-1017", "maintainer": "Git", "description": "Self-hosted Git repositories.", "beta": false, "ctl_uninstall": true},
	{"id": "HyperBackup", "name": "Hyper Backup", "version": "3.0.2-2446", "maintainer": "Synology Inc.", "description": "Multi-version backup to local + cloud destinations.", "beta": false, "ctl_uninstall": true},
	{"id": "ActiveBackup-Business", "name": "Active Backup for Business", "version": "2.6.2-12517", "maintainer": "Synology Inc.", "description": "Centralized backup for PCs, servers, and VMs.", "beta": false, "ctl_uninstall": true},
	{"id": "MediaServer", "name": "Media Server", "version": "2.0.5-3152", "maintainer": "Synology Inc.", "description": "DLNA media server.", "beta": false, "ctl_uninstall": true},
	{"id": "NoteStation", "name": "Note Station", "version": "2.8.2-3508", "maintainer": "Synology Inc.", "description": "Take + sync notes between devices.", "beta": false, "ctl_uninstall": true},
	{"id": "Node.js_v18", "name": "Node.js v18", "version": "18.18.2-0011", "maintainer": "nodejs.org", "description": "Node.js runtime v18.", "beta": false, "ctl_uninstall": true},
	{"id": "OAuthService", "name": "OAuth Service", "version": "1.1.2-0071", "maintainer": "Synology Inc.", "description": "OAuth 2.0 provider for DSM accounts.", "beta": false, "ctl_uninstall": true},
	{"id": "Plex", "name": "Plex Media Server", "version": "1.41.5.9626-72180", "maintainer": "Plex Inc", "description": "Stream your media to any device.", "beta": false, "ctl_uninstall": true},
	{"id": "Python3", "name": "Python3", "version": "3.9.14-0003", "maintainer": "Python Software Foundation", "description": "Python 3 interpreter.", "beta": false, "ctl_uninstall": true},
	{"id": "SMBService", "name": "SMB Service", "version": "4.15.13-0330", "maintainer": "Synology Inc.", "description": "SMB / CIFS file sharing.", "beta": false, "ctl_uninstall": false},
	{"id": "StorageAnalyzer", "name": "Storage Analyzer", "version": "2.1.0-0421", "maintainer": "Synology Inc.", "description": "Detailed storage usage breakdowns.", "beta": false, "ctl_uninstall": true},
	{"id": "SurveillanceStation", "name": "Surveillance Station", "version": "9.2.5-11979", "maintainer": "Synology Inc.", "description": "IP camera management + recording.", "beta": false, "ctl_uninstall": true},
	{"id": "SynologyApplicationService", "name": "Synology Application Service", "version": "1.7.2-10549", "maintainer": "Synology Inc.", "description": "Shared service backend for Synology apps.", "beta": false, "ctl_uninstall": true},
	{"id": "SynologyDrive", "name": "Synology Drive Server", "version": "3.1.0-22920", "maintainer": "Synology Inc.", "description": "File sync + collaboration.", "beta": false, "ctl_uninstall": true},
	{"id": "SynologyPhotos", "name": "Synology Photos", "version": "1.3.4-0340", "maintainer": "Synology Inc.", "description": "Photo management with face + subject AI.", "beta": false, "ctl_uninstall": true},
	{"id": "Tailscale", "name": "Tailscale", "version": "1.58.2-700058002", "maintainer": "Tailscale, Inc.", "description": "Zero-config VPN built on WireGuard.", "beta": false, "ctl_uninstall": true},
	{"id": "VideoStation", "name": "Video Station", "version": "3.0.7-2512", "maintainer": "Synology Inc.", "description": "Organise + stream video collections.", "beta": false, "ctl_uninstall": true},
	{"id": "iTunesServer", "name": "iTunes Server", "version": "2.0.0-2723", "maintainer": "Synology Inc.", "description": "Share music via the iTunes protocol.", "beta": false, "ctl_uninstall": true},
}

// — package catalog (Available) —

func avail(id, name, ver, maintainer, desc string, size int64) map[string]any {
	return map[string]any{"id": id, "package": id, "name": name, "version": ver, "maintainer": maintainer, "desc": desc, "size": size}
}

var demoCatalog = []map[string]any{
	avail("AntiVirus", "AntiVirus Essential", "1.5.4-3099", "Synology Inc.", "Signature-based malware scanning for DSM shared folders.", 234_881_024),
	avail("Apache2.4", "Apache HTTP Server 2.4", "2.4.54-0125", "apache.org", "Run Apache 2.4 alongside the built-in nginx.", 1_785_446),
	avail("CalendarServer", "Calendar", "2.4.6-10942", "Synology Inc.", "CalDAV server + DSM calendar UI.", 7_858_345),
	avail("Chat", "Chat", "2.4.2-12125", "Synology Inc.", "Team chat hosted on your NAS.", 70_254_530),
	avail("CloudStation", "Cloud Station Backup", "2.7.2-2464", "Synology Inc.", "Versioned backup target for client devices.", 5_871_546),
	avail("ContactsServer", "Contacts", "1.0.5-10492", "Synology Inc.", "CardDAV address-book server.", 8_283_135),
	avail("DDB", "Data Deposit Box", "0.1-700350", "Data Deposit Box Inc.", "Cloud backup target (DDB).", 5_558_310),
	avail("DirectoryServer", "Directory Server", "2.4.57-2653", "Synology Inc.", "LDAP-compatible directory service.", 1_572_864),
	avail("Emby", "Emby Server", "4.7.14.0-70405", "Emby LLC", "Media server (Plex alternative).", 74_516_004),
	avail("GlacierBackup", "Glacier Backup", "1.5.2-1143", "Synology Inc.", "Backup to AWS Glacier cold storage.", 1_785_446),
	avail("HyperBackupVault", "Hyper Backup Vault", "3.0.2-2446", "Synology Inc.", "Receive Hyper Backups from other Synology NAS units.", 4_809_768),
	avail("MailPlus-Server", "MailPlus Server", "3.2.0-21029", "Synology Inc.", "Full-stack mail server (SMTP + IMAP + AV).", 198_412_416),
	avail("MailStation", "Mail Station", "20231031-10324", "Synology Inc.", "Webmail front-end.", 4_298_752),
	avail("MariaDB10", "MariaDB 10", "10.3.32-1040", "MariaDB Foundation", "Drop-in MySQL replacement.", 59_768_832),
	avail("MediaWiki", "MediaWiki", "1.39.2-1078", "MediaWiki", "Self-hosted wiki software.", 40_894_464),
	avail("PDFViewer", "PDF Viewer", "1.2.3-1124", "Synology Inc.", "In-browser PDF reader.", 798_720),
	avail("PHP8.0", "PHP 8.0", "8.0.23-0103", "php.net", "PHP runtime, v8.0 line.", 12_582_912),
	avail("RadiusServer", "RADIUS Server", "3.0.27-0453", "Synology Inc.", "RADIUS authentication backend.", 1_677_722),
	avail("ScsiTarget", "SAN Manager", "1.0.2-0207", "Synology Inc.", "iSCSI / Fibre Channel target manager.", 4_194_304),
	avail("Spreadsheet", "Synology Office", "3.4.2-18342", "Synology Inc.", "Browser-based docs + spreadsheets + slides.", 124_780_544),
	avail("TeamViewer", "TeamViewer", "3.5.10-7005", "TeamViewer Germany GmbH", "Remote-control your NAS shell.", 16_777_216),
	avail("TextEditor", "Text Editor", "1.2.4-0245", "Synology Inc.", "Lightweight in-browser text editor.", 2_097_152),
	avail("USBCopy", "USB Copy", "2.2.1-1103", "Synology Inc.", "One-touch external-drive sync.", 1_363_148),
	avail("VPNCenter", "VPN Server", "1.4.4-2855", "Synology Inc.", "Run OpenVPN / L2TP / PPTP server.", 2_516_582),
	avail("WebDAVServer", "WebDAV Server", "2.4.8-10135", "Synology Inc.", "Expose shares via WebDAV.", 1_677_722),
	avail("WebStation", "Web Station", "3.0.0-0309", "Synology Inc.", "Host websites with nginx + Apache + PHP.", 14_680_064),
	avail("WordPress", "WordPress", "6.1.1-1062", "WordPress", "Blogging + CMS platform.", 16_777_216),
	// — Synology first-party (rest) —
	avail("Moments", "Moments", "1.5.3-1018", "Synology Inc.", "Legacy photo-management app (replaced by Synology Photos).", 84_215_040),
	avail("Office", "Synology Office", "3.4.2-18342", "Synology Inc.", "Collaborative docs / sheets / slides for DSM users.", 134_217_728),
	avail("Calendar", "Synology Calendar", "2.4.6-10942", "Synology Inc.", "CalDAV server with a built-in DSM client.", 18_891_581),
	avail("Note-Station", "Note Station", "2.8.2-3508", "Synology Inc.", "Take, organize, and share rich-text notes.", 92_274_688),
	avail("DownloadStation", "Download Station", "3.9.5-4627", "Synology Inc.", "BitTorrent + HTTP/FTP download manager.", 28_311_552),
	avail("AudioStation", "Audio Station", "7.0.3-5401", "Synology Inc.", "Stream your music library to any device.", 47_185_920),
	avail("VideoStation", "Video Station", "3.0.7-2512", "Synology Inc.", "Organise + stream your video collection.", 142_606_336),
	avail("MediaServer", "Media Server", "2.0.5-3152", "Synology Inc.", "DLNA media server for TVs and consoles.", 6_815_744),
	avail("Photos", "Synology Photos", "1.3.4-0340", "Synology Inc.", "AI photo management with face + subject detection.", 198_180_864),
	avail("Drive", "Synology Drive Server", "3.1.0-22920", "Synology Inc.", "File sync + collaboration across devices.", 84_934_656),
	avail("DriveShareSync", "Drive ShareSync", "3.1.0-22920", "Synology Inc.", "Cross-NAS share replication for Synology Drive.", 21_233_664),
	avail("Universal-Search", "Universal Search", "1.5.2-0516", "Synology Inc.", "Search across packages + files + apps.", 7_340_032),
	avail("ActiveBackup", "Active Backup for Business", "2.6.2-12517", "Synology Inc.", "Centralized backup for PCs, servers, and VMs.", 198_180_864),
	avail("HyperBackup", "Hyper Backup", "3.0.2-2446", "Synology Inc.", "Multi-version backup to local + cloud destinations.", 78_643_200),
	avail("CloudSync", "Cloud Sync", "2.7.2-2464", "Synology Inc.", "Two-way sync with Dropbox / Drive / S3 / etc.", 25_165_824),
	avail("HybridShare", "Hybrid Share", "1.3.0-08025", "Synology Inc.", "Cache-tier link between your NAS and Synology C2.", 16_777_216),
	avail("PresentationStation", "Presentation Station", "1.0.3-1014", "Synology Inc.", "Slideshow + signage hosting (deprecated).", 4_194_304),
	avail("DSM-Guide", "DSM Guide", "7.2.2-0072", "Synology Inc.", "Inline help + walk-throughs for DSM 7.", 31_457_280),
	avail("Help", "Help", "7.2.2-0072", "Synology Inc.", "DSM help system + manuals.", 18_874_368),
	avail("Container-Manager", "Container Manager", "24.0.2-1535", "Synology Inc.", "Manage Docker containers and Compose stacks.", 167_772_160),
	avail("SurveillanceStation", "Surveillance Station", "9.2.5-11979", "Synology Inc.", "IP camera management + recording (2 licenses included).", 254_217_728),
	avail("StorageAnalyzer", "Storage Analyzer", "2.1.0-0421", "Synology Inc.", "Detailed storage usage breakdowns + scheduled reports.", 1_835_008),
	avail("Synology-iSCSI-Manager", "Storage Manager", "1.0.2-0207", "Synology Inc.", "Manage volumes, pools, iSCSI targets, and HDD/SSD health.", 4_194_304),
	avail("SnapshotReplication", "Snapshot Replication", "1.2.5-0612", "Synology Inc.", "Periodic Btrfs snapshot replication to another NAS.", 12_582_912),
	avail("USBCopy", "USB Copy", "2.2.1-1103", "Synology Inc.", "One-touch external-drive sync.", 1_363_148),
	avail("VPNServer", "VPN Server", "1.4.4-2855", "Synology Inc.", "OpenVPN / L2TP / PPTP server.", 2_516_582),
	avail("VMM", "Virtual Machine Manager", "2.6.6-19073", "Synology Inc.", "Run KVM virtual machines on DSM.", 254_217_728),
	avail("Domain-Server", "Synology Directory Server", "4.10.18-0330", "Synology Inc.", "Samba-based Active Directory–compatible domain.", 76_546_048),
	avail("DNS-Server", "DNS Server", "2.2.5-0354", "Synology Inc.", "BIND-based authoritative DNS.", 6_553_600),
	avail("DHCP-Server", "DHCP Server", "1.0.1-0036", "Synology Inc.", "ISC dhcpd front-end with reservations + leases UI.", 1_363_148),
	avail("Proxy-Server", "Proxy Server", "1.4.4-0233", "Synology Inc.", "Squid HTTP forward-proxy front-end.", 6_291_456),
	avail("Log-Center", "Log Center", "1.2.3-0290", "Synology Inc.", "Aggregate syslogs from network devices.", 18_350_080),
	avail("Resource-Monitor", "Resource Monitor", "1.0.0-0001", "Synology Inc.", "Historical CPU/RAM/disk/network monitoring.", 6_291_456),
	avail("SecureSignIn", "Secure SignIn", "1.0.3-0138", "Synology Inc.", "Hardware-key + push-approval login for DSM.", 12_582_912),
	avail("OAuth-Service", "OAuth Service", "1.1.2-0071", "Synology Inc.", "OAuth 2.0 provider for DSM accounts.", 1_572_864),
	avail("Synology-Application-Service", "Synology Application Service", "1.7.2-10549", "Synology Inc.", "Shared service backend for first-party Synology apps.", 18_874_368),
	avail("AdvancedMediaExtensions", "Advanced Media Extensions", "1.1.2-0301", "Synology Inc.", "HEVC / HEIC / etc. codec extensions for DSM apps.", 65_536_000),
	avail("Replication-Service", "Replication Service", "1.4.2-2103", "Synology Inc.", "Real-time block-level replication between NAS units.", 14_680_064),
	avail("HighAvailability", "Synology High Availability", "2.1.2-2901", "Synology Inc.", "Active-passive HA failover (2 NAS units required).", 14_680_064),
	avail("Active-Insight", "Active Insight", "1.2.0-1214", "Synology Inc.", "Cloud monitoring service for Synology NAS.", 4_194_304),
	avail("CMS", "Central Management System", "1.4.0-1009", "Synology Inc.", "Manage multiple Synology NAS units from one console.", 6_291_456),
	avail("WebDAVServer-pkg", "WebDAV Server", "2.4.8-10135", "Synology Inc.", "Expose shares via WebDAV.", 1_677_722),
	// — Web stack / runtimes —
	avail("PHP7.3", "PHP 7.3", "7.3.33-0165", "php.net", "PHP runtime, v7.3 line.", 12_582_912),
	avail("PHP7.4", "PHP 7.4", "7.4.30-0118", "php.net", "PHP runtime, v7.4 line.", 12_582_912),
	avail("PHP8.1", "PHP 8.1", "8.1.27-0103", "php.net", "PHP runtime, v8.1 line.", 12_582_912),
	avail("PHP8.2", "PHP 8.2", "8.2.15-0103", "php.net", "PHP runtime, v8.2 line.", 12_582_912),
	avail("Node.js_v14", "Node.js v14", "14.21.3-1014", "nodejs.org", "Node.js runtime v14 (LTS, EOL Apr 2023).", 13_631_488),
	avail("Node.js_v16", "Node.js v16", "16.20.2-1014", "nodejs.org", "Node.js runtime v16 (LTS, EOL Sep 2023).", 12_582_912),
	avail("Node.js_v18", "Node.js v18", "18.18.2-0011", "nodejs.org", "Node.js runtime v18 (current LTS).", 13_631_488),
	avail("Node.js_v20", "Node.js v20", "20.10.0-1031", "nodejs.org", "Node.js runtime v20 (current LTS).", 14_680_064),
	avail("Python2", "Python2", "2.7.18-1004", "Python Software Foundation", "Legacy Python 2 runtime (EOL Jan 2020).", 5_242_880),
	avail("Python3.9", "Python3.9", "3.9.14-0003", "Python Software Foundation", "Python 3.9 runtime.", 9_437_184),
	avail("Perl", "Perl", "5.28.3-0231", "Perl.org", "Perl 5 runtime.", 20_971_520),
	avail("Git", "Git Server", "2.39.1-1017", "Git", "Self-hosted Git repositories.", 4_718_592),
	avail("Java8", "Java 8 (OpenJDK)", "8.0.302-0143", "Eclipse Foundation", "OpenJDK 8 runtime for Synology apps that need it.", 178_257_920),
	avail("Java11", "Java 11 (OpenJDK)", "11.0.20-0143", "Eclipse Foundation", "OpenJDK 11 runtime.", 198_180_864),
	// — Third-party media servers —
	avail("Plex", "Plex Media Server", "1.41.5.9626-72180", "Plex Inc", "Stream your media to any device.", 98_566_144),
	avail("Jellyfin", "Jellyfin", "10.9.0-0001", "Jellyfin Project", "Free + open-source media server.", 156_237_824),
	avail("AirVideoHD", "Air Video HD", "1.5.0-0001", "InMethod", "Stream video to iOS / tvOS clients.", 4_718_592),
	// — Backup / sync —
	avail("Resilio-Sync", "Resilio Sync", "2.7.3-2", "Resilio Inc.", "Peer-to-peer file sync (BitTorrent Sync successor).", 8_388_608),
	avail("Syncthing", "Syncthing", "1.27.2-0001", "syncthing.net", "Open-source continuous file sync.", 31_457_280),
	avail("GoodSync", "GoodSync", "12.5.8.8-1375", "https://www.goodsync.com", "Cross-platform sync + backup.", 16_777_216),
	avail("rclone", "rclone", "1.65.1-0001", "rclone.org", "Sync to 40+ cloud storage providers.", 47_185_920),
	// — Databases / queues —
	avail("MariaDB10", "MariaDB 10", "10.3.32-1040", "MariaDB Foundation", "Drop-in MySQL replacement.", 59_768_832),
	avail("PostgreSQL15", "PostgreSQL 15", "15.5-0001", "PostgreSQL Global Dev Group", "Object-relational database.", 64_487_424),
	avail("Redis", "Redis", "7.2.4-0001", "Redis Ltd.", "In-memory data store.", 8_388_608),
	avail("MongoDB", "MongoDB", "6.0.12-0001", "MongoDB Inc.", "Document-oriented NoSQL database.", 124_780_544),
	avail("RabbitMQ", "RabbitMQ", "3.12.10-0001", "VMware", "AMQP message broker.", 84_934_656),
	// — Wikis / web apps —
	avail("MediaWiki", "MediaWiki", "1.39.2-1078", "MediaWiki", "Self-hosted wiki software.", 40_894_464),
	avail("DokuWiki", "DokuWiki", "2024-02-06a-0001", "DokuWiki", "Flat-file wiki software.", 14_680_064),
	avail("phpMyAdmin", "phpMyAdmin", "5.2.1-1078", "phpMyAdmin devel team", "Web-based MariaDB / MySQL admin.", 7_340_032),
	avail("Joomla", "Joomla", "4.2.9-1079", "Joomla Leadership Team", "PHP-based content-management system.", 19_922_944),
	avail("Drupal", "Drupal", "10.2.0-0001", "Drupal Association", "PHP-based content-management framework.", 23_068_672),
	avail("Magento", "Magento", "2.4.6-0001", "Adobe", "PHP e-commerce platform.", 178_257_920),
	avail("phpBB", "phpBB", "3.3.11-0001", "phpBB Limited", "PHP forum software.", 16_777_216),
	avail("KodCloud", "KodExplorer", "4.51-702304", "KodCloud", "Web-based file manager.", 10_485_760),
	avail("OwnCloud", "ownCloud", "10.13.4-0001", "ownCloud GmbH", "Self-hosted file collaboration.", 84_934_656),
	avail("Nextcloud", "Nextcloud", "27.1.5-0001", "Nextcloud GmbH", "Drive / Calendar / Contacts / Mail (fork of ownCloud).", 124_780_544),
	avail("Vtiger", "vtiger CRM", "7.5.0-1063", "Vtiger", "PHP-based CRM platform.", 52_428_800),
	avail("SuiteCRM", "SuiteCRM", "8.5.0-0001", "SalesAgility", "PHP-based CRM platform.", 64_487_424),
	// — Backup / sync extras —
	avail("Veeam-Agent", "Veeam Agent for Linux", "5.1.0-0001", "Veeam Software", "Endpoint backup of Linux clients.", 88_080_384),
	// — Misc —
	avail("Tailscale", "Tailscale", "1.58.2-700058002", "Tailscale, Inc.", "Zero-config VPN built on WireGuard.", 23_068_672),
	avail("Domotz", "Domotz", "3.7.1-700570", "Domotz Inc", "Network monitoring + management.", 47_185_920),
	avail("ElephantDrive", "ElephantDrive", "7.0-7014", "ElephantDrive Inc.", "Cloud backup target (ElephantDrive).", 7_340_032),
	avail("IDrive", "IDrive", "2.06.04-7023", "IDrive Inc.", "Cloud backup target (IDrive).", 4_927_488),
	avail("NBR", "Nakivo Backup & Replication", "11.1.0.98634-1", "Nakivo Inc.", "Enterprise backup / replication / VM protection.", 511_705_088),
	avail("NBR-Transporter", "Nakivo Transporter", "11.1.0.98634-1", "Nakivo Inc.", "Companion data-mover for Nakivo.", 126_877_696),
	avail("Mega", "MEGAcmd", "1.4.0-1000", "Mega Ltd.", "CLI client + sync agent for MEGA.nz.", 35_651_584),
	avail("MinimServer", "MinimServer", "2.2-7001", "MinimWorld Ltd", "UPnP/DLNA media server with deep tagging support.", 19_922_944),
	avail("PDFViewer", "PDF Viewer", "1.2.3-1124", "Synology Inc.", "In-browser PDF reader.", 798_720),
	avail("VirtualHere", "VirtualHere", "4.6.6-1466", "VirtualHere Pty. Ltd.", "Share USB devices over the network.", 2_359_296),
	avail("exFAT-Free", "exFAT (Free)", "7.0.0-0204", "Synology Inc.", "Free exFAT driver bundle for DSM.", 131_072),
	avail("iTunesServer", "iTunes Server", "2.0.0-2723", "Synology Inc.", "Share music via the iTunes protocol.", 731_136),
	avail("Mosquitto", "Mosquitto MQTT Broker", "2.0.18-0001", "Eclipse Foundation", "Lightweight MQTT broker for home automation.", 6_291_456),
	avail("HomeAssistant-Companion", "Home Assistant Companion", "0.5.0-0001", "Nabu Casa", "Helper bridge for Home Assistant on DSM.", 4_194_304),
}

// — services —

var demoServices = []map[string]any{
	{"service": "nfs-server", "display_name": "NFS", "display_name_section_key": "nfs", "enable_status": "enabled"},
	{"service": "smb", "display_name": "SMB / CIFS", "display_name_section_key": "smb", "enable_status": "enabled"},
	{"service": "sftp", "display_name": "sftp", "display_name_section_key": "sftp", "enable_status": "disabled"},
	{"service": "ssh-shell", "display_name": "ssh-shell", "display_name_section_key": "ssh", "enable_status": "enabled"},
	{"service": "telnetd", "display_name": "Telnet", "display_name_section_key": "telnet", "enable_status": "disabled"},
	{"service": "ftp-pure", "display_name": "FTP", "display_name_section_key": "ftp", "enable_status": "disabled"},
	{"service": "tftp", "display_name": "TFTP", "display_name_section_key": "tftp", "enable_status": "disabled"},
	{"service": "snmpd", "display_name": "SNMP", "display_name_section_key": "snmp", "enable_status": "always-on"},
	{"service": "ntpd", "display_name": "NTP", "display_name_section_key": "ntp", "enable_status": "enabled"},
	{"service": "bonjour", "display_name": "Bonjour mDNS", "display_name_section_key": "bonjour", "enable_status": "disabled"},
	{"service": "webdav", "display_name": "WebDAV", "display_name_section_key": "webdav", "enable_status": "enabled"},
	{"service": "upnp", "display_name": "UPnP", "display_name_section_key": "upnp", "enable_status": "enabled"},
	{"service": "rsyncd", "display_name": "rsyncd", "display_name_section_key": "rsync", "enable_status": "disabled"},
	{"service": "cupsd", "display_name": "CUPS print daemon", "display_name_section_key": "cups", "enable_status": "always-on"},
	{"service": "synoscgi", "display_name": "synoscgi", "display_name_section_key": "synoscgi", "enable_status": "always-on"},
	{"service": "pkg-iscsi", "display_name": "iSCSI", "display_name_section_key": "iscsi", "enable_status": "always-on"},
	{"service": "ups-net", "display_name": "ups-net", "display_name_section_key": "ups", "enable_status": "disabled"},
	{"service": "ups-usb", "display_name": "ups-usb", "display_name_section_key": "ups", "enable_status": "always-on"},
	{"service": "pkg-synosamba-wstransfer-genc", "display_name": "WS-Discovery", "display_name_section_key": "ws", "enable_status": "enabled"},
}

// — containers (Docker) —

var demoContainers = []map[string]any{
	{"id": "a1b2c3d4e5f6", "name": "plex", "image": "linuxserver/plex:latest", "status": "running", "cpu": 18, "mem": 412_384_000, "created": time.Now().Add(-12 * 24 * time.Hour).Unix(), "ports": "32400:32400/tcp"},
	{"id": "f6e5d4c3b2a1", "name": "jellyfin", "image": "jellyfin/jellyfin:10.9", "status": "running", "cpu": 6, "mem": 312_400_000, "created": time.Now().Add(-9 * 24 * time.Hour).Unix(), "ports": "8096:8096/tcp"},
	{"id": "1234567890ab", "name": "homepage", "image": "ghcr.io/gethomepage/homepage:v0.9.2", "status": "running", "cpu": 1, "mem": 28_412_000, "created": time.Now().Add(-22 * 24 * time.Hour).Unix(), "ports": "3000:3000/tcp"},
	{"id": "ba0987654321", "name": "pihole", "image": "pihole/pihole:latest", "status": "stopped", "cpu": 0, "mem": 0, "created": time.Now().Add(-100 * 24 * time.Hour).Unix(), "ports": "53:53/udp,80:8081/tcp"},
	{"id": "cafebabe1234", "name": "traefik", "image": "traefik:v3.0", "status": "running", "cpu": 2, "mem": 64_412_000, "created": time.Now().Add(-30 * 24 * time.Hour).Unix(), "ports": "80:80/tcp,443:443/tcp"},
	{"id": "deadbeef5678", "name": "uptime-kuma", "image": "louislam/uptime-kuma:1", "status": "running", "cpu": 1, "mem": 132_140_000, "created": time.Now().Add(-40 * 24 * time.Hour).Unix(), "ports": "3001:3001/tcp"},
}

// dsm.Image fields are repository / tag / repotag — *not* "name". Setting
// just "name" leaves the rendered row showing ":latest" because the view
// reads repository+tag as the display label. We populate all three for
// forward-compat across firmware shapes.
func img(id, repo, tag string, size int64, ageDays int, in_use int, containers int) map[string]any {
	return map[string]any{
		"id": id, "repository": repo, "tag": tag, "repotag": repo + ":" + tag,
		"size": size, "virtual_size": size + size/10,
		"created": time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour).Unix(),
		"in_use":  in_use, "containers": containers,
	}
}

var demoDockerImages = []map[string]any{
	img("sha256:plex01234", "linuxserver/plex", "latest", 388_412_408, 12, 1, 1),
	img("sha256:jellyf5678", "jellyfin/jellyfin", "10.9", 412_840_000, 9, 1, 1),
	img("sha256:homepage9a", "ghcr.io/gethomepage/homepage", "v0.9.2", 280_412_000, 22, 1, 1),
	img("sha256:pihole2024", "pihole/pihole", "latest", 198_412_000, 100, 0, 1),
	img("sha256:traefikv3c", "traefik", "v3.0", 88_240_000, 30, 1, 1),
	img("sha256:kumab37cd1", "louislam/uptime-kuma", "1", 312_140_000, 40, 1, 1),
	img("sha256:nginx9c1f", "nginx", "alpine", 42_140_000, 14, 0, 0),
	img("sha256:alpine0ab9", "alpine", "3.20", 7_840_000, 60, 0, 0),
	img("sha256:postgres17", "postgres", "16", 412_140_000, 35, 0, 0),
	img("sha256:redis_alpc", "redis", "7-alpine", 38_412_000, 14, 0, 0),
	img("sha256:photoprism", "photoprism/photoprism", "latest", 624_140_000, 70, 0, 0),
	img("sha256:vaultwarden", "vaultwarden/server", "1.30", 178_412_000, 90, 0, 0),
}

var demoDockerNetworks = []map[string]any{
	{"id": "net_bridge", "name": "bridge", "driver": "bridge", "scope": "local", "containers": 4},
	{"id": "net_host", "name": "host", "driver": "host", "scope": "local", "containers": 1},
	{"id": "net_homelab", "name": "homelab", "driver": "bridge", "scope": "local", "containers": 3},
}

// — surveillance —

var demoCameras = []map[string]any{
	{"id": 1, "name": "Living Room", "model": "Reolink RLC-823A", "vendor": "Reolink", "status": 1, "newName": "Living Room"},
	{"id": 2, "name": "Garage", "model": "Hikvision DS-2CD2143G2", "vendor": "Hikvision", "status": 1, "newName": "Garage"},
	{"id": 3, "name": "Front Door", "model": "Reolink Argus 3 Pro", "vendor": "Reolink", "status": 7, "newName": "Front Door"},
	{"id": 4, "name": "Back Garden", "model": "Reolink RLC-810A", "vendor": "Reolink", "status": 1, "newName": "Back Garden"},
}

var demoRecordings = []map[string]any{
	{"id": 121, "camera_name": "Living Room", "start_time": time.Now().Add(-2 * time.Hour).Unix(), "length": 1842},
	{"id": 122, "camera_name": "Garage", "start_time": time.Now().Add(-3 * time.Hour).Unix(), "length": 1280},
	{"id": 123, "camera_name": "Front Door", "start_time": time.Now().Add(-5 * time.Hour).Unix(), "length": 482},
}

var demoSurveillanceInfo = map[string]any{
	"version":       map[string]any{"version": "9.2.5-11979", "build": "11979"},
	"path":          "/volume2/@surveillance",
	"hostname":      "demo-ds923",
	"cameras_total": 4, "cameras_online": 3,
}

// — backup —

var demoHyperBackupTasks = []map[string]any{
	{"task_id": 1, "name": "Daily homes → /volume2/backups", "target": "Synology NAS (local)", "schedule": "daily 03:00", "last_run": time.Now().Add(-90 * time.Minute).Unix(), "last_status": "success", "total_size": 980_412_416_416},
	{"task_id": 2, "name": "Monthly cold storage → external", "target": "External USB", "schedule": "monthly", "last_run": time.Now().Add(-13 * time.Hour).Unix(), "last_status": "success", "total_size": 4_120_412_416_416},
	{"task_id": 3, "name": "Photos → B2", "target": "Backblaze B2", "schedule": "weekly Sun 02:00", "last_run": time.Now().Add(-6 * 24 * time.Hour).Unix(), "last_status": "warning", "total_size": 1_265_404_887_040},
}

var demoActiveBackupTasks = []map[string]any{
	{"task_id": 11, "name": "laptop-jan", "source_type": "device", "target": "/volume2/active-backup", "last_run": time.Now().Add(-22 * time.Hour).Unix(), "status": "success"},
	{"task_id": 12, "name": "laptop-kasia", "source_type": "device", "target": "/volume2/active-backup", "last_run": time.Now().Add(-48 * time.Hour).Unix(), "status": "success"},
	{"task_id": 13, "name": "vm-homelab", "source_type": "vm", "target": "/volume2/active-backup", "last_run": time.Now().Add(-10 * 24 * time.Hour).Unix(), "status": "failed"},
}

var demoActiveBackupVersions = []map[string]any{
	{"version_id": 4002, "task_id": 11, "time": time.Now().Add(-22 * time.Hour).Unix(), "size": 1_240_412_416, "status": "complete"},
	{"version_id": 4001, "task_id": 11, "time": time.Now().Add(-46 * time.Hour).Unix(), "size": 980_412_416, "status": "complete"},
	{"version_id": 4000, "task_id": 11, "time": time.Now().Add(-72 * time.Hour).Unix(), "size": 1_540_412_416, "status": "complete"},
}

// — drive —

var demoDriveFiles = []map[string]any{
	{"name": "Q4-planning.gdoc", "path": "/Drive/Q4-planning.gdoc", "size": 124_280, "type": "document", "modified": time.Now().Add(-6 * time.Hour).Unix(), "owner": "baraniewski"},
	{"name": "homelab-roadmap.gsheet", "path": "/Drive/homelab-roadmap.gsheet", "size": 88_412, "type": "spreadsheet", "modified": time.Now().Add(-2 * 24 * time.Hour).Unix(), "owner": "baraniewski"},
	{"name": "talk-slides.gslides", "path": "/Drive/talk-slides.gslides", "size": 1_412_408, "type": "presentation", "modified": time.Now().Add(-4 * 24 * time.Hour).Unix(), "owner": "baraniewski"},
	{"name": "video-2026-05-01.mp4", "path": "/Drive/video-2026-05-01.mp4", "size": 482_412_408, "type": "video", "modified": time.Now().Add(-18 * 24 * time.Hour).Unix(), "owner": "demo"},
}

var demoDriveStats = map[string]any{
	"total_users": 5, "total_files": 18_412, "storage_used": 12_412_840_000, "storage_quota": 100_000_000_000,
}

// — security —

func cert(id, cn, issuer string, expiresIn time.Duration, isDefault bool) map[string]any {
	return map[string]any{
		"id": id, "common_name": cn, "issuer": map[string]any{"common_name": issuer},
		"subject":          map[string]any{"common_name": cn},
		"subject_alt_name": []string{cn, "*." + cn},
		"valid_from":       time.Now().Add(-365 * 24 * time.Hour).Format(time.RFC3339),
		"valid_till":       time.Now().Add(expiresIn).Format(time.RFC3339),
		"is_default":       isDefault, "is_broken": false,
		"services": []string{"system", "webdav"},
	}
}

var demoCertificates = []map[string]any{
	cert("default", "demo-ds923.local", "synology", 8*365*24*time.Hour, false),
	cert("letsencrypt-1", "homelab.example.com", "Let's Encrypt R3", 60*24*time.Hour, true),
	cert("letsencrypt-2", "vpn.example.com", "Let's Encrypt R3", 8*24*time.Hour, false), // expiry warn
	cert("self-signed-old", "old-host.lan", "Synology Inc.", -7*24*time.Hour, false),    // already expired
}

var demoSecAdvisorConf = map[string]any{
	"last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix(),
	"total_items":  6, "critical": 1, "warn": 2, "info": 3,
}

var demoSecAdvisorItems = []map[string]any{
	{"id": "ssh_any", "severity": "critical", "title": "SSH allowed from any IP", "description": "Restrict SSH to LAN or specific subnets to reduce brute-force exposure.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
	{"id": "auto_block_off", "severity": "warn", "title": "Auto Block disabled", "description": "Enable Auto Block to ban IPs after repeated failed logins.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
	{"id": "password_policy", "severity": "warn", "title": "Weak password policy", "description": "Increase minimum password length to 12 and require mixed case + digits.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
	{"id": "ntp_sync", "severity": "info", "title": "NTP healthy", "description": "Time synchronisation is within 1 second of the configured pool.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
	{"id": "dsm_update", "severity": "info", "title": "DSM up to date", "description": "Running latest DSM 7.2.2-72806 Update 3.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
	{"id": "2fa_enrolled", "severity": "info", "title": "2-step verification enrolled for admin", "description": "Admin accounts have 2FA enabled.", "last_scanned": time.Now().Add(-3 * 24 * time.Hour).Unix()},
}

var demoSchedTasks = []map[string]any{
	{"id": 1, "name": "Antivirus weekly scan", "type": "antivirus", "enable": true, "next_trigger_time": time.Now().Add(4 * 24 * time.Hour).Unix(), "owner": "system", "repeat": "weekly"},
	{"id": 2, "name": "Synology Drive index rebuild", "type": "package", "enable": true, "next_trigger_time": time.Now().Add(20 * 24 * time.Hour).Unix(), "owner": "system", "repeat": "monthly"},
	{"id": 3, "name": "Photo upload script", "type": "user-defined", "enable": false, "next_trigger_time": 0, "owner": "baraniewski", "repeat": "daily"},
	{"id": 4, "name": "Quarterly cold-storage rotation", "type": "user-defined", "enable": true, "next_trigger_time": time.Now().Add(80 * 24 * time.Hour).Unix(), "owner": "baraniewski", "repeat": "quarterly"},
	{"id": 5, "name": "S.M.A.R.T. extended test", "type": "smart", "enable": true, "next_trigger_time": time.Now().Add(11 * 24 * time.Hour).Unix(), "owner": "system", "repeat": "monthly"},
}

var demoFirewallStatus = map[string]any{
	"enabled": true, "profile": "Default", "notify_via_dsm_notify": true,
}

var demoFirewallProfiles = []map[string]any{
	{"name": "Default", "is_default": true, "enabled": true, "rules": 8},
	{"name": "Strict (away)", "is_default": false, "enabled": false, "rules": 12},
}

var demoFirewallRules = []map[string]any{
	{"id": 1, "name": "Allow LAN", "enabled": true, "action": "allow", "protocol": "all", "source": "192.168.1.0/24", "dest_port": "all"},
	{"id": 2, "name": "Allow Tailscale", "enabled": true, "action": "allow", "protocol": "all", "source": "100.64.0.0/10", "dest_port": "all"},
	{"id": 3, "name": "Allow HTTPS", "enabled": true, "action": "allow", "protocol": "tcp", "source": "any", "dest_port": "443"},
	{"id": 4, "name": "Allow HTTP", "enabled": true, "action": "allow", "protocol": "tcp", "source": "any", "dest_port": "80"},
	{"id": 5, "name": "Allow SSH from LAN only", "enabled": true, "action": "allow", "protocol": "tcp", "source": "192.168.1.0/24", "dest_port": "22"},
	{"id": 6, "name": "Block country: RU", "enabled": true, "action": "deny", "protocol": "all", "source": "geo:RU", "dest_port": "all"},
	{"id": 7, "name": "Block country: CN", "enabled": true, "action": "deny", "protocol": "all", "source": "geo:CN", "dest_port": "all"},
	{"id": 8, "name": "Default deny", "enabled": true, "action": "deny", "protocol": "all", "source": "any", "dest_port": "all"},
}

var demoDDNSProviders = []map[string]any{
	{"name": "Synology", "url": "checkip.synology.com"},
	{"name": "DynDNS", "url": "checkip.dyndns.com"},
	{"name": "No-IP", "url": "dynupdate.no-ip.com"},
	{"name": "Cloudflare", "url": "api.cloudflare.com"},
}

var demoDDNSRecords = []map[string]any{
	{"hostname": "demo-ds923.synology.me", "provider": "Synology", "ipv4": "203.0.113.42", "ipv6": "2001:db8::42", "status": "OK", "last_updated": time.Now().Add(-15 * time.Minute).Unix(), "enabled": true},
	{"hostname": "homelab.dyndns.org", "provider": "DynDNS", "ipv4": "203.0.113.42", "ipv6": "", "status": "OK", "last_updated": time.Now().Add(-32 * time.Minute).Unix(), "enabled": true},
	{"hostname": "vpn.cloudflare.net", "provider": "Cloudflare", "ipv4": "203.0.113.42", "ipv6": "2001:db8::42", "status": "OK", "last_updated": time.Now().Add(-5 * time.Minute).Unix(), "enabled": true},
}
