package dsm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FSEntry is one row from SYNO.FileStation.List.list (a file or folder).
type FSEntry struct {
	IsDir bool   `json:"isdir"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type,omitempty"`
	Add   struct {
		Size       int64     `json:"size"`
		Owner      OwnerInfo `json:"owner,omitempty"`
		Time       FSTime    `json:"time,omitempty"`
		Perm       FSPerm    `json:"perm,omitempty"`
		RealPath   string    `json:"real_path,omitempty"`
		Type       string    `json:"type,omitempty"`
		MountPoint string    `json:"mount_point_type,omitempty"`
	} `json:"additional"`
}

// OwnerInfo captures DSM's user/group ownership block.
type OwnerInfo struct {
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
	UID   int    `json:"uid,omitempty"`
	GID   int    `json:"gid,omitempty"`
}

// FSTime captures DSM's timestamps (epoch seconds).
type FSTime struct {
	Atime  int64 `json:"atime,omitempty"`
	Mtime  int64 `json:"mtime,omitempty"`
	Ctime  int64 `json:"ctime,omitempty"`
	Crtime int64 `json:"crtime,omitempty"`
}

// FSPerm captures filesystem permission flags.
type FSPerm struct {
	POSIX    int `json:"posix"`
	AdvRight any `json:"adv_right,omitempty"`
	ACL      any `json:"acl,omitempty"`
}

// VolumeStatus is the per-share filesystem space block returned in the
// FileStation list_share `additional.volume_status` field.
type VolumeStatus struct {
	FreeSpace  int64 `json:"freespace,omitempty"`
	TotalSpace int64 `json:"totalspace,omitempty"`
	ReadOnly   bool  `json:"readonly,omitempty"`
}

// FileShare is one entry from SYNO.FileStation.List.list_share — the
// roots File Station exposes (typically the shared folders).
type FileShare struct {
	IsDir bool   `json:"isdir"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Add   struct {
		Owner     OwnerInfo    `json:"owner,omitempty"`
		Time      FSTime       `json:"time,omitempty"`
		Perm      FSPerm       `json:"perm,omitempty"`
		MountType string       `json:"mount_point_type,omitempty"`
		VolStatus VolumeStatus `json:"volume_status,omitempty"`
		RealPath  string       `json:"real_path,omitempty"`
	} `json:"additional"`
}

// FileShares lists the File Station roots (top-level shares). This is
// the natural entry point for the file browser.
func (c *Client) FileShares(ctx context.Context) ([]FileShare, error) {
	params := url.Values{}
	params.Set("additional", `["real_path","owner","time","perm","mount_point_type","volume_status"]`)
	var resp struct {
		Shares []FileShare `json:"shares"`
		Total  int         `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.FileStation.List", 2, "list_share", params, &resp); err != nil {
		return nil, err
	}
	return resp.Shares, nil
}

// FileDownload streams a file from FileStation. The caller closes the
// reader. Returns the raw stream — Content-Disposition is exposed
// separately for callers that want the server's suggested filename.
func (c *Client) FileDownload(ctx context.Context, path string) (rc io.ReadCloser, contentDisposition string, err error) {
	params := url.Values{}
	params.Set("path", jsonStringArray(path))
	params.Set("mode", "download")
	return c.RawCall(ctx, "SYNO.FileStation.Download", 2, "download", params)
}

// FileDelete deletes a single file or directory. Path must be the
// absolute DSM path (e.g. /volume1/photos/foo.jpg). `recursive` is
// required for non-empty directories.
func (c *Client) FileDelete(ctx context.Context, path string, recursive bool) error {
	params := url.Values{}
	params.Set("path", jsonStringArray(path))
	if recursive {
		params.Set("recursive", "true")
	}
	return c.Call(ctx, "SYNO.FileStation.Delete", 2, "delete", params, nil)
}

// FileRename renames a file in place. newName is the final path component
// (not a full path).
func (c *Client) FileRename(ctx context.Context, path, newName string) error {
	params := url.Values{}
	params.Set("path", jsonStringArray(path))
	params.Set("name", jsonStringArray(newName))
	return c.Call(ctx, "SYNO.FileStation.Rename", 2, "rename", params, nil)
}

// uploadMaxSize caps the single-shot upload at 64 MiB. DSM accepts
// larger files, but anything above that should go through the
// chunked upload protocol (start_chunked / append_chunked /
// finish_chunked) which we haven't wired yet. The limit is a
// guardrail, not a DSM constraint.
const uploadMaxSize int64 = 64 << 20

// Upload pushes a local file into a DSM shared folder via
// SYNO.FileStation.Upload v2 `upload`. The endpoint is multipart
// form-encoded — not the usual application/x-www-form-urlencoded
// the rest of this package uses — so we bypass Client.Call and
// build the request manually.
//
// dstFolder must be the DSM-style absolute folder path
// (e.g. /volume1/photos). localPath is the on-host source file.
// When overwrite is true, an existing same-named file at the
// destination is replaced; otherwise DSM returns a 1805 error and
// we surface it through the standard *Error envelope.
//
// TODO: chunked upload. For now we cap at 64 MiB single-shot —
// larger inputs return an error rather than trying and failing
// halfway through. The chunked variant uses three calls
// (start_chunked, append_chunked, finish_chunked) plus a server-
// assigned upload id; that flow needs the TUI to surface a real
// progress bar, which is a separate piece of work.
func (c *Client) Upload(ctx context.Context, dstFolder, localPath string, overwrite bool) error {
	if dstFolder == "" {
		return fmt.Errorf("dsm: destination folder is required")
	}
	if localPath == "" {
		return fmt.Errorf("dsm: local path is required")
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("dsm: open %s: %w", localPath, err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("dsm: stat %s: %w", localPath, err)
	}
	if stat.IsDir() {
		return fmt.Errorf("dsm: %s is a directory; upload one file at a time", localPath)
	}
	if stat.Size() > uploadMaxSize {
		return fmt.Errorf("dsm: %s is %d bytes; single-shot upload caps at %d (chunked upload TODO)", localPath, stat.Size(), uploadMaxSize)
	}

	// Build the multipart body. DSM expects the metadata fields
	// (api/version/method/path/overwrite) as ordinary form parts
	// *before* the binary `file` part — order matters on some
	// firmware revisions.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	writeField := func(name, value string) error {
		fw, err := mw.CreateFormField(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(fw, value)
		return err
	}
	if err := writeField("api", "SYNO.FileStation.Upload"); err != nil {
		return err
	}
	if err := writeField("version", "2"); err != nil {
		return err
	}
	if err := writeField("method", "upload"); err != nil {
		return err
	}
	if err := writeField("path", dstFolder); err != nil {
		return err
	}
	if err := writeField("overwrite", strconv.FormatBool(overwrite)); err != nil {
		return err
	}
	if err := writeField("create_parents", "true"); err != nil {
		return err
	}
	if sid := c.SID(); sid != "" {
		if err := writeField("_sid", sid); err != nil {
			return err
		}
	}

	// The binary `file` part. We set the Content-Type explicitly —
	// DSM rejects octet-stream-only parts on some builds.
	filename := filepath.Base(localPath)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	header.Set("Content-Type", "application/octet-stream")
	part, err := mw.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("dsm: copy body: %w", err)
	}
	if err := mw.Close(); err != nil {
		return err
	}

	endpoint := *c.baseURL
	endpoint.Path = c.pathFor("SYNO.FileStation.Upload")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tok := c.token(); tok != "" {
		req.Header.Set("X-SYNO-TOKEN", tok)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dsm: upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dsm: upload: http %d", resp.StatusCode)
	}

	// DSM still wraps the upload response in the standard envelope.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("dsm: upload: read response: %w", err)
	}
	if err := parseUploadResponse(respBody); err != nil {
		return err
	}
	return nil
}

// parseUploadResponse decodes the envelope returned by FileStation.Upload.
func parseUploadResponse(body []byte) error {
	// Reuse the package-internal envelope type via a lightweight
	// inline shape so we don't expose it.
	var env struct {
		Success bool `json:"success"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error,omitempty"`
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("dsm: upload: empty response")
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("dsm: upload: parse envelope: %w", err)
	}
	if !env.Success {
		code := 0
		if env.Error != nil {
			code = env.Error.Code
		}
		return &Error{Code: code, API: "SYNO.FileStation.Upload", Method: "upload"}
	}
	return nil
}

// Download fetches a remote DSM file and writes it to localPath.
// Uses the existing RawCall plumbing (FileDownload underneath) so
// the binary stream stays out of the JSON-envelope path. When DSM
// surfaces a Content-Disposition header we honour it: if localPath
// names an existing directory, the file lands inside it with the
// server-suggested filename.
//
// This is the inverse of Upload and shares its "no progress
// callback yet" caveat — large downloads block until they finish
// or the context elapses.
func (c *Client) Download(ctx context.Context, remotePath, localPath string) error {
	if remotePath == "" {
		return fmt.Errorf("dsm: remote path is required")
	}
	if localPath == "" {
		return fmt.Errorf("dsm: local path is required")
	}

	rc, disposition, err := c.FileDownload(ctx, remotePath)
	if err != nil {
		return err
	}
	defer rc.Close()

	// If localPath points at an existing directory, derive the
	// filename: prefer the server-supplied one, fall back to the
	// remote path basename.
	target := localPath
	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		name := filenameFromDisposition(disposition)
		if name == "" {
			name = filepath.Base(remotePath)
		}
		target = filepath.Join(localPath, name)
	}

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("dsm: create %s: %w", target, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("dsm: download body: %w", err)
	}
	return nil
}

// filenameFromDisposition pulls the filename hint out of a
// Content-Disposition header. We support the common
// `attachment; filename="foo.tar"` shape — RFC 5987's filename*
// encoded variant isn't worth the dependency here.
func filenameFromDisposition(disposition string) string {
	if disposition == "" {
		return ""
	}
	parts := strings.Split(disposition, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "filename=") {
			name := strings.TrimPrefix(part, "filename=")
			name = strings.Trim(name, `"`)
			return name
		}
	}
	return ""
}

// ListFiles lists the contents of a folder. Path is the DSM-style
// absolute path (e.g. /volume1/photos). The result is a slice of FSEntry
// with size + timestamps populated.
func (c *Client) ListFiles(ctx context.Context, path string, offset, limit int) ([]FSEntry, int, error) {
	params := url.Values{}
	params.Set("folder_path", path)
	params.Set("offset", strconv.Itoa(offset))
	if limit <= 0 {
		limit = 500
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("sort_by", "name")
	params.Set("additional", `["real_path","size","owner","time","perm","type"]`)
	var resp struct {
		Files []FSEntry `json:"files"`
		Total int       `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.FileStation.List", 2, "list", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Files, resp.Total, nil
}

// DirSizeResult is the final summary returned by SYNO.FileStation.DirSize
// after `start` → `status` polling completes.
type DirSizeResult struct {
	NumDirs  int64 `json:"num_dir"`
	NumFiles int64 `json:"num_file"`
	Total    int64 `json:"total_size"`
}

// DirSize computes the on-disk size of a directory by running the DSM
// async task SYNO.FileStation.DirSize: `start` returns a task id, then
// `status` is polled until the server reports finished=true.
//
// The call blocks until the task completes or ctx is cancelled. DirSize
// is expensive (walks the tree server-side) so callers should cache the
// result rather than re-running it on every render.
func (c *Client) DirSize(ctx context.Context, dirPath string) (DirSizeResult, error) {
	startParams := url.Values{}
	startParams.Set("path", jsonStringArray(dirPath))
	var start struct {
		TaskID string `json:"taskid"`
	}
	if err := c.Call(ctx, "SYNO.FileStation.DirSize", 2, "start", startParams, &start); err != nil {
		return DirSizeResult{}, err
	}
	if start.TaskID == "" {
		return DirSizeResult{}, fmt.Errorf("dsm: DirSize: empty taskid")
	}
	// Poll status. DSM doesn't expose a long-poll variant so we sleep
	// between checks; the cadence is conservative because most folders
	// settle in under a second and we don't want to hammer the box.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	statusParams := url.Values{}
	statusParams.Set("taskid", start.TaskID)
	for {
		var status struct {
			Finished bool  `json:"finished"`
			NumDirs  int64 `json:"num_dir"`
			NumFiles int64 `json:"num_file"`
			Total    int64 `json:"total_size"`
		}
		if err := c.Call(ctx, "SYNO.FileStation.DirSize", 2, "status", statusParams, &status); err != nil {
			return DirSizeResult{}, err
		}
		if status.Finished {
			return DirSizeResult{NumDirs: status.NumDirs, NumFiles: status.NumFiles, Total: status.Total}, nil
		}
		select {
		case <-ctx.Done():
			return DirSizeResult{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func jsonStringArray(values ...string) string {
	b, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(b)
}
