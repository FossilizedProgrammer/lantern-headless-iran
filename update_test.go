package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/flashlight/v7/apipb"
	"github.com/getlantern/flashlight/v7/common"
)

func TestUpdatePlanAuto(t *testing.T) {
	args.UpstreamProxy = ""
	plan, err := updatePlan(viaAuto)
	require.NoError(t, err)
	require.Equal(t, []string{viaDirect}, plan)

	args.UpstreamProxy = "socks5://127.0.0.1:1080"
	plan, err = updatePlan(viaAuto)
	require.NoError(t, err)
	require.Equal(t, []string{viaUpstream, viaDirect}, plan)
	args.UpstreamProxy = ""
}

func TestUpdatePlanExplicit(t *testing.T) {
	for _, via := range []string{viaUpstream, viaDirect} {
		plan, err := updatePlan(via)
		require.NoError(t, err)
		require.Equal(t, []string{via}, plan)
	}
}

func TestUpdatePlanUnknown(t *testing.T) {
	_, err := updatePlan("carrier-pigeon")
	require.Error(t, err)

	// the fronting transport was removed
	_, err = updatePlan("fronting")
	require.Error(t, err)
}

func TestTryPlanFirstSuccessStops(t *testing.T) {
	confBytes, err := proto.Marshal(testConfigResponse())
	require.NoError(t, err)

	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(confBytes)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	out, err := tryPlan([]string{viaDirect}, ts.URL, uc, 10*time.Second)
	require.NoError(t, err)
	require.True(t, out.ok)
	require.False(t, out.unchanged)
	require.Equal(t, viaDirect, out.via)
	require.Equal(t, 1, requests)
}

func TestTryPlanAllFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	out, err := tryPlan([]string{viaDirect, viaDirect}, ts.URL, uc, 10*time.Second)
	require.Error(t, err)
	require.False(t, out.ok)
}

func TestTryPlanUnchanged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	out, err := tryPlan([]string{viaDirect}, ts.URL, uc, 10*time.Second)
	require.NoError(t, err)
	require.True(t, out.ok)
	require.True(t, out.unchanged)
	require.Nil(t, out.conf)
}

func TestBuildConfigRequest(t *testing.T) {
	uc := common.NewUserConfigData("lantern-headless", "device-123", 42, "tok", nil, "en-US")
	req, err := buildConfigRequest("https://df.iantem.io/api/v1/config", uc, []string{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "https://df.iantem.io/api/v1/config", req.URL.String())
	require.Equal(t, "application/x-protobuf", req.Header.Get("Content-Type"))
	require.Equal(t, "no-cache", req.Header.Get("Cache-Control"))
	require.Equal(t, "device-123", req.Header.Get(common.DeviceIdHeader))
	require.Equal(t, "42", req.Header.Get(common.UserIdHeader))
	require.Equal(t, "tok", req.Header.Get(common.ProTokenHeader))

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var cr apipb.ConfigRequest
	require.NoError(t, proto.Unmarshal(body, &cr))
	require.Equal(t, []string{"a", "b"}, cr.GetProxy().GetNames())
	require.Equal(t, "42", cr.GetClientInfo().GetUserId())
	require.Equal(t, "tok", cr.GetClientInfo().GetProToken())
}

func testProxy(name, addr string) *apipb.ProxyConnectConfig {
	return &apipb.ProxyConnectConfig{
		Name: name,
		Addr: addr,
		Port: 443,
		ProtocolConfig: &apipb.ProxyConnectConfig_ConnectCfgTls{
			ConnectCfgTls: &apipb.ProxyConnectConfig_TLSConfig{},
		},
	}
}

func testConfigResponse() *apipb.ConfigResponse {
	return &apipb.ConfigResponse{
		Proxy: &apipb.ConfigResponse_Proxy{
			Proxies: []*apipb.ProxyConnectConfig{testProxy("sunny-tokyo-02", "203.0.113.10")},
		},
	}
}

func TestFetchServerConfigDirect(t *testing.T) {
	confBytes, err := proto.Marshal(testConfigResponse())
	require.NoError(t, err)

	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		var cr apipb.ConfigRequest
		require.NoError(t, proto.Unmarshal(body, &cr))
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(confBytes)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	got, unchanged, err := fetchServerConfig(viaDirect, ts.URL, uc, 10*time.Second)
	require.NoError(t, err)
	require.False(t, unchanged)
	require.NotNil(t, got)
	require.Len(t, got.GetProxy().GetProxies(), 1)
	require.Equal(t, 1, requests)
}

func TestFetchServerConfigUnchanged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	got, unchanged, err := fetchServerConfig(viaDirect, ts.URL, uc, 10*time.Second)
	require.NoError(t, err)
	require.True(t, unchanged)
	require.Nil(t, got)
}

func TestFetchServerConfigBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	_, _, err := fetchServerConfig(viaDirect, ts.URL, uc, 10*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestFetchServerConfigUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	_, _, err := fetchServerConfig(viaDirect, ts.URL, uc, 10*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth login")
}

func TestFetchServerConfigGarbageBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer ts.Close()

	args.DataPath = t.TempDir()
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	_, _, err := fetchServerConfig(viaDirect, ts.URL, uc, 10*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not with a Lantern config")
}

func TestFetchServerConfigUnknownTransport(t *testing.T) {
	uc := common.NewUserConfigData("lantern-headless", "device-123", 1, "tok", nil, "en-US")
	_, _, err := fetchServerConfig("pigeon", "https://df.iantem.io/api/v1/config", uc, time.Second)
	require.Error(t, err)
}

func TestSaveUserConfigRoundtrip(t *testing.T) {
	args.DataPath = t.TempDir()
	require.NoError(t, saveUserConfig(testConfigResponse()))
	proxies, err := readSavedProxies()
	require.NoError(t, err)
	require.Len(t, proxies, 1)
	require.Equal(t, "203.0.113.10:443", proxies["sunny-tokyo-02"].Addr)
}

func TestSavedProxyNamesSorted(t *testing.T) {
	args.DataPath = t.TempDir()
	require.NoError(t, saveUserConfig(&apipb.ConfigResponse{
		Proxy: &apipb.ConfigResponse_Proxy{
			Proxies: []*apipb.ProxyConnectConfig{
				testProxy("zebra", "203.0.113.1"),
				testProxy("alpha", "203.0.113.2"),
			},
		},
	}))
	require.Equal(t, []string{"alpha", "zebra"}, savedProxyNames())
}

func TestUpstreamClientSchemeValidation(t *testing.T) {
	_, err := upstreamClient("ftp://127.0.0.1:1080", time.Second)
	require.Error(t, err)

	_, err = upstreamClient("::::", time.Second)
	require.Error(t, err)
}
