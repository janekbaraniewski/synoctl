package views

import "github.com/janbaraniewski/synology-ctl/internal/dsm"

// Result messages shared by every page. Keeping them in one place lets
// us delete the old single-purpose view files without losing the
// envelopes they used to define.

type utilMsg struct {
	U   *dsm.Utilization
	Err error
}

type storageMsg struct {
	S   *dsm.Storage
	Err error
}

type procsMsg struct {
	P   []dsm.Process
	Err error
}

type recentLogsMsg struct {
	L   []dsm.LogEntry
	Err error
}

type sharesMsg struct {
	S   []dsm.Share
	Err error
}

type filesListMsg struct {
	Path  string
	E     []dsm.FSEntry
	Total int
	Err   error
}

type usersMsg struct {
	U   []dsm.User
	Err error
}

type netMsg struct {
	I   []dsm.NetworkInterface
	Err error
}

type packagesMsg struct {
	P   []dsm.Package
	Err error
}

type pkgActionMsg struct {
	ID, Action string
	Err        error
}

type servicesMsg struct {
	S   []dsm.Service
	Err error
}

type svcActionMsg struct {
	ID, Action string
	Err        error
}

type sysViewInfoMsg struct {
	I   *dsm.SystemInfo
	Err error
}

type sysViewUtilMsg struct {
	U   *dsm.Utilization
	Err error
}
