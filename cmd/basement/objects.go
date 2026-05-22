// Package main — objects.go implements the data-plane subcommands:
//
//	basement objects list   BUCKET [--prefix PFX]
//	basement objects get    BUCKET KEY [--output FILE]
//	basement objects put    BUCKET KEY FILE
//	basement objects delete BUCKET KEY
//
// Endpoints (all region-scoped — see internal/api/user_regions.go):
//
//	GET    /user/regions/{rid}/buckets/{bid}/objects
//	GET    /user/regions/{rid}/buckets/{bid}/objects/{key}/presign-get
//	POST   /user/regions/{rid}/buckets/{bid}/objects/{key}/presign-put
//	DELETE /user/regions/{rid}/buckets/{bid}/objects/{key}
//
// get and put presign first, then stream the bytes directly to/from
// the backend via the returned URL — the CLI never proxies object
// bytes through basement, mirroring the web UI's pattern.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// objectInfo mirrors driver.ObjectInfo on the wire.
type objectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	IsDir        bool      `json:"is_dir,omitempty"`
}

type objectPage struct {
	Objects          []objectInfo `json:"objects"`
	NextContinuation string       `json:"nextContinuation,omitempty"`
	IsTruncated      bool         `json:"isTruncated"`
	CommonPrefixes   []string     `json:"commonPrefixes,omitempty"`
}

type presignedURL struct {
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"`
	Method  string    `json:"method"`
}

func newObjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "objects",
		Short: "List, download, upload, or delete objects.",
	}
	cmd.AddCommand(newObjectsListCmd())
	cmd.AddCommand(newObjectsGetCmd())
	cmd.AddCommand(newObjectsPutCmd())
	cmd.AddCommand(newObjectsDeleteCmd())
	return cmd
}

func newObjectsListCmd() *cobra.Command {
	var prefix string
	cmd := &cobra.Command{
		Use:   "list BUCKET",
		Short: "List objects in a bucket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, p, err := clientForProfile()
			if err != nil {
				return err
			}
			rid, err := regionID(p)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/user/regions/%s/buckets/%s/objects", rid, args[0])
			// Empty delimiter = flat list — handy for scripts. The web
			// UI uses delimiter="/" for folder navigation; the CLI is
			// flat by default since most automation wants the whole
			// tree.
			path += "?delimiter="
			if prefix != "" {
				path += "&prefix=" + prefix
			}
			var out objectPage
			if err := client.GetJSON(context.Background(), path, &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			rows := make([][]string, 0, len(out.Objects))
			for _, o := range out.Objects {
				rows = append(rows, []string{o.Key, fmt.Sprintf("%d", o.Size), o.LastModified.Format(time.RFC3339)})
			}
			renderTable(cmd.OutOrStdout(), []string{"KEY", "SIZE", "LAST_MODIFIED"}, rows)
			if out.IsTruncated {
				fmt.Fprintln(cmd.OutOrStdout(), "(truncated)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Filter objects by key prefix")
	return cmd
}

func newObjectsGetCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get BUCKET KEY",
		Short: "Download an object to stdout or --output FILE.",
		Long: `Presigns a GET URL via basement, then streams the bytes
directly from the backend. The presigned URL is short-lived (default
1h server-side); the download is bounded by --timeout (default 5m).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, p, err := clientForProfile()
			if err != nil {
				return err
			}
			rid, err := regionID(p)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/user/regions/%s/buckets/%s/objects/%s/presign-get",
				rid, args[0], pathEscape(args[1]))
			var presign presignedURL
			if err := client.GetJSON(context.Background(), path, &presign); err != nil {
				return err
			}
			return streamFromURL(presign.URL, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default stdout)")
	return cmd
}

func newObjectsPutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "put BUCKET KEY FILE",
		Short: "Upload a local file to a bucket key.",
		Long: `Presigns a PUT URL via basement, then streams the local
file directly to the backend. Use "-" as FILE to read from stdin
(content-length must be known — this still requires a real local
file for now).`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, p, err := clientForProfile()
			if err != nil {
				return err
			}
			rid, err := regionID(p)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/user/regions/%s/buckets/%s/objects/%s/presign-put",
				rid, args[0], pathEscape(args[1]))
			var presign presignedURL
			// presign-put accepts an optional {contentType} body — we
			// leave it empty (server defaults to application/octet-stream).
			if err := client.PostJSON(context.Background(), path, map[string]string{}, &presign); err != nil {
				return err
			}
			return streamToURL(presign.URL, args[2])
		},
	}
}

func newObjectsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete BUCKET KEY",
		Short: "Delete an object.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, p, err := clientForProfile()
			if err != nil {
				return err
			}
			rid, err := regionID(p)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/user/regions/%s/buckets/%s/objects/%s",
				rid, args[0], pathEscape(args[1]))
			if err := client.DeleteJSON(context.Background(), path, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s/%s\n", args[0], args[1])
			return nil
		},
	}
}

// streamFromURL downloads the presigned URL to either stdout (output
// == "") or the given path. The HTTP client is fresh (not the bearer
// client) because the backend rejects extra Authorization headers on
// presigned URLs.
func streamFromURL(url, output string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download HTTP %d: %s", resp.StatusCode, string(body))
	}
	var w io.Writer = os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("create %s: %w", output, err)
		}
		defer f.Close()
		w = f
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// streamToURL uploads file to the presigned PUT URL. Content-Length
// is set explicitly so signature-v4 sees the same number of bytes the
// server signed. Uses a 30m timeout — large uploads are legitimate.
func streamToURL(url, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open %s: %w", file, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", file, err)
	}
	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return err
	}
	req.ContentLength = st.Size()
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

