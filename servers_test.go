package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonconfig "github.com/getlantern/common/config"
	"github.com/getlantern/flashlight/v7/apipb"
)

func testProxyMap() map[string]*commonconfig.ProxyConfig {
	return map[string]*commonconfig.ProxyConfig{
		"sunny-tokyo-02": {
			Name: "sunny-tokyo-02",
			Addr: "203.0.113.10:443",
			Location: &commonconfig.ProxyConfig_ProxyLocation{
				City: "Tokyo", Country: "Japan", CountryCode: "JP",
			},
		},
		"flaky-frankfurt-01": {
			Name: "flaky-frankfurt-01",
			Addr: "198.51.100.10:443",
			Location: &commonconfig.ProxyConfig_ProxyLocation{
				City: "Frankfurt", Country: "Germany", CountryCode: "DE",
			},
		},
	}
}

func TestServerFilterKeepsOnlyNamedServer(t *testing.T) {
	filter := serverFilter("sunny-tokyo-02")
	filtered := filter(testProxyMap())
	require.Len(t, filtered, 1)
	require.Contains(t, filtered, "sunny-tokyo-02")
}

func TestServerFilterIsCaseInsensitive(t *testing.T) {
	filter := serverFilter("SUNNY-TOKYO-02")
	filtered := filter(testProxyMap())
	require.Len(t, filtered, 1)
	require.Contains(t, filtered, "sunny-tokyo-02")
}

func TestServerFilterDropsAllWhenNoMatch(t *testing.T) {
	filter := serverFilter("does-not-exist")
	require.Empty(t, filter(testProxyMap()))
	require.Empty(t, filter(nil))
}

func TestToServerInfosSortedByCountryThenName(t *testing.T) {
	infos := toServerInfos(testProxyMap())
	require.Len(t, infos, 2)
	require.Equal(t, "flaky-frankfurt-01", infos[0].Name)
	require.Equal(t, "DE", infos[0].CountryCode)
	require.Equal(t, "sunny-tokyo-02", infos[1].Name)
	require.Equal(t, "JP", infos[1].CountryCode)
}

func TestReadSavedProxiesProtoAndJSON(t *testing.T) {
	for _, format := range []string{"proto", "json"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, savedUserConfigName)
			conf := &apipb.ConfigResponse{
				Proxy: &apipb.ConfigResponse_Proxy{
					Proxies: []*apipb.ProxyConnectConfig{{
						Name: "sunny-tokyo-02",
						Addr: "203.0.113.10",
						Port: 443,
						Location: &apipb.ProxyConnectConfig_ProxyLocation{
							City: "Tokyo", Country: "Japan", CountryCode: "JP",
						},
						ProtocolConfig: &apipb.ProxyConnectConfig_ConnectCfgTls{
							ConnectCfgTls: &apipb.ProxyConnectConfig_TLSConfig{},
						},
					}},
				},
			}
			var data []byte
			var err error
			if format == "json" {
				data, err = protojson.Marshal(conf)
			} else {
				data, err = proto.Marshal(conf)
			}
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data, 0600))

			args.DataPath = dir
			proxies, err := readSavedProxies()
			require.NoError(t, err)
			require.Len(t, proxies, 1)
			p := proxies["sunny-tokyo-02"]
			require.NotNil(t, p)
			require.Equal(t, "203.0.113.10:443", p.Addr)
			require.Equal(t, "JP", p.Location.CountryCode)
		})
	}
}

func TestReadSavedProxiesMissingFile(t *testing.T) {
	args.DataPath = t.TempDir()
	proxies, err := readSavedProxies()
	require.NoError(t, err)
	require.Empty(t, proxies)
}

func TestProbeServers(t *testing.T) {
	// A real listener on localhost.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	open := ln.Addr().String()

	// A port with (almost certainly) nothing listening.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closed := ln2.Addr().String()
	require.NoError(t, ln2.Close())

	proxies := map[string]*commonconfig.ProxyConfig{
		"open":  {Name: "open", Addr: open},
		"dead":  {Name: "dead", Addr: closed},
		"empty": {Name: "empty"},
	}
	res := probeServers(proxies, 2*time.Second)
	require.Len(t, res, 2)
	require.True(t, res[open])
	require.False(t, res[closed])
}

func TestWithReachability(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	proxies := map[string]*commonconfig.ProxyConfig{
		"a": {Name: "a", Addr: ln.Addr().String()},
	}
	infos := withReachability(toServerInfos(proxies), proxies, 2*time.Second)
	require.Len(t, infos, 1)
	require.NotNil(t, infos[0].Reachable)
	require.True(t, *infos[0].Reachable)

	// Without a check, Reachable stays unset and is omitted from JSON.
	plain, err := json.Marshal(toServerInfos(proxies))
	require.NoError(t, err)
	require.NotContains(t, string(plain), "reachable")
}
