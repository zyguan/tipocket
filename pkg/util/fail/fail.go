package fail

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"

	"golang.org/x/sync/errgroup"
)

const (
	// KindPD defines pd kind string
	KindPD = "pd"
	// KindTiKV defines tikv kind string
	KindTiKV = "tikv"
	// KindTiDB defines tidb kind string
	KindTiDB = "tidb"
)

// Presets define some common used presets
var Presets = map[string]Preset{
	"async-commit": {
		TiDB: map[string]string{
			"github.com/pingcap/tidb/store/tikv/prewritePrimaryFail":        "5%return",
			"github.com/pingcap/tidb/store/tikv/prewriteSecondaryFail":      "2%return",
			"github.com/pingcap/tidb/store/tikv/shortPessimisticLockTTL":    "20%return",
			"github.com/pingcap/tidb/store/tikv/twoPCShortLockTTL":          "20%return",
			"github.com/pingcap/tidb/store/tikv/commitFailedSkipCleanup":    "30%return",
			"github.com/pingcap/tidb/store/tikv/twoPCRequestBatchSizeLimit": "30%return",
			"github.com/pingcap/tidb/store/tikv/invalidMaxCommitTS":         "10%return",
			"github.com/pingcap/tidb/store/tikv/asyncCommitDoNothing":       "10%return",
			"github.com/pingcap/tidb/store/tikv/rpcFailOnSend":              `2%return("write")`,
			"github.com/pingcap/tidb/store/tikv/rpcFailOnRecv":              `2%return("write")`,
			"github.com/pingcap/tidb/store/tikv/noRetryOnRpcError":          "20%return(true)",
			"github.com/pingcap/tidb/store/tikv/beforeCommit":               `10%return("delay")->10%return("fail")`,
			"github.com/pingcap/tidb/store/tikv/doNotKeepAlive":             "50%return",
			"github.com/pingcap/tidb/store/tikv/snapshotGetTSAsync":         "5%sleep(100)",
		},
		TiKV: map[string]string{
			"cm_after_read_key_check":         "1%sleep(100)",
			"cm_after_read_range_check":       "1%sleep(100)",
			"delay_update_max_ts":             "10%return",
			"after_calculate_min_commit_ts":   "2%sleep(100)",
			"async_commit_1pc_force_fallback": "2%return",
		},
	},
}

// Preset defines a group of failpoints
type Preset struct {
	PD   map[string]string
	TiKV map[string]string
	TiDB map[string]string
}

// Client is a client for managing failpoints
type Client struct {
	endpoints map[string][]*url.URL
	http      *http.Client
}

// New creates a failpoint client
func New() *Client {
	return &Client{endpoints: make(map[string][]*url.URL), http: http.DefaultClient}
}

// AddEndpoint registers target server to given kind group
func (cli *Client) AddEndpoint(kind string, host string, port int, optPath ...string) error {
	path := "/fail/"
	if len(optPath) > 0 {
		path = optPath[0]
	}
	ep := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", host, port), Path: path}
	if _, err := doFailRequest(cli.http, ep.String(), http.MethodGet); err != nil {
		return fmt.Errorf("failed to add %q as a %s fail endpoint: %s", ep.String(), kind, err.Error())
	}
	cli.endpoints[kind] = append(cli.endpoints[kind], ep)
	return nil
}

// Set can be used to enable/disable a failpoint on all servers in given kind
func (cli *Client) Set(kind string, failpoint string, action string) error {
	var g errgroup.Group
	for i := range cli.endpoints[kind] {
		api := cli.failAPI(kind, i, failpoint)
		g.Go(func() error {
			var (
				err    error
				method string
				opts   []func(*bodyOptions)
			)
			if len(action) > 0 {
				method = http.MethodPut
				opts = append(opts, withBody(bytes.NewBufferString(action)))
			} else {
				method = http.MethodDelete
			}
			_, err = doFailRequest(cli.http, api, method, opts...)
			if err != nil {
				return fmt.Errorf("%s %s: %s", method, api, err.Error())
			}
			return nil
		})
	}
	return g.Wait()
}

// Enable enables all failpoints in the preset
func (cli *Client) Enable(preset Preset, failfast bool) error {
	var fstErr error
	for _, g := range []struct {
		kind       string
		failpoints map[string]string
	}{
		{KindPD, preset.PD},
		{KindTiKV, preset.TiKV},
		{KindTiDB, preset.TiDB},
	} {
		for k, v := range g.failpoints {
			if err := cli.Set(g.kind, k, v); err != nil && failfast {
				return fmt.Errorf("failed to set %s failpoint: %s=%q", g.kind, k, v)
			} else if err != nil && fstErr == nil {
				fstErr = err
			}
		}
	}
	return fstErr
}

// Disable disables all failpoints in the preset
func (cli *Client) Disable(preset Preset, failfast bool) error {
	var fstErr error
	for _, g := range []struct {
		kind       string
		failpoints map[string]string
	}{
		{KindPD, preset.PD},
		{KindTiKV, preset.TiKV},
		{KindTiDB, preset.TiDB},
	} {
		for k := range g.failpoints {
			if err := cli.Set(g.kind, k, ""); err != nil && failfast {
				return fmt.Errorf("failed to unset %s failpoint: %s", g.kind, k)
			} else if err != nil && fstErr == nil {
				fstErr = err
			}
		}
	}
	return fstErr
}

func (cli *Client) failAPI(kind string, i int, failpoint string) string {
	api := *cli.endpoints[kind][i]
	api.Path = path.Join(api.Path, failpoint)
	return api.String()
}

func doFailRequest(cli *http.Client, url string, method string, opts ...func(*bodyOptions)) ([]byte, error) {
	if method == "" {
		method = http.MethodGet
	}
	b := &bodyOptions{}
	for _, f := range opts {
		f(b)
	}
	req, err := http.NewRequest(method, url, b.body)
	if err != nil {
		return nil, err
	}
	if b.contentType != "" {
		req.Header.Set("Content-Type", b.contentType)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// TODO tidb failpoint http handler will return 204 status code
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		msg, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("[%d] %s", resp.StatusCode, msg)
	}
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return content, nil
}

type bodyOptions struct {
	contentType string
	body        io.Reader
}

func withBody(body io.Reader) func(*bodyOptions) {
	return func(opts *bodyOptions) {
		opts.body = body
	}
}
func withContentType(contentType string) func(*bodyOptions) {
	return func(opts *bodyOptions) {
		opts.contentType = contentType
	}
}
