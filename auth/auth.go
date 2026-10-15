package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pterm/pterm"
	"google.golang.org/protobuf/encoding/protowire"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type authClient struct {
	*http.Client
	baseURL string
}

type Client interface {
	SignUp(ctx context.Context, email string, password string) ([]byte, error)
	SignupEmailResendCode(ctx context.Context, data *SignupEmailResendRequest) (bool, error)
	SignupEmailConfirmation(ctx context.Context, data *ConfirmSignupRequest) (bool, error)
	GetSalt(ctx context.Context, email string) (*GetSaltResponse, error)
	LoginPrepare(ctx context.Context, loginData *PrepareRequest) (*PrepareResponse, error)
	Login(ctx context.Context, email string, password string, deviceId string) (*LoginResponse, []byte, error)
	SignOut(ctx context.Context, logoutData *LogoutRequest) (bool, error)
}

// const DefaultAPIURL = "https://api.iantem.io/v1"
const DefaultAPIURL = "https://df.iantem.io/api/v1"

// clientOptions configures how the auth HTTP client reaches the network.
type clientOptions struct {
	upstreamProxy string
}

// Option customizes the auth client.
type Option func(*clientOptions)

// WithUpstreamProxy makes the auth client reach the API through the given
// HTTP/HTTPS/SOCKS5 proxy. Without it the client connects directly.
func WithUpstreamProxy(proxy string) Option {
	return func(c *clientOptions) { c.upstreamProxy = proxy }
}

func NewClient(baseURL string, insecure bool, opts ...Option) Client {
	co := &clientOptions{}
	for _, o := range opts {
		o(co)
	}
	transport := &http.Transport{}
	if co.upstreamProxy != "" {
		u, err := url.Parse(co.upstreamProxy)
		if err != nil || u.Scheme == "" {
			pterm.Error.Printfln("Invalid upstream proxy %q, ignoring: %v", co.upstreamProxy, err)
		} else {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	if insecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
	return &authClient{httpClient, baseURL}
}

// parseErrorBody extracts (code, message) from the protobuf error body the
// API returns on non-200 responses: field 1 varint = code, field 2 string =
// message.
func parseErrorBody(b []byte) (int32, string) {
	var code int32
	var msg string
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return code, msg
		}
		b = b[n:]
		switch typ {
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return code, msg
			}
			if num == 1 {
				code = int32(v)
			}
			b = b[n:]
		case protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return code, msg
			}
			if num == 2 {
				msg = string(v)
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return code, msg
			}
			b = b[n:]
		}
	}
	return code, msg
}

func (c *authClient) GetPROTOC(ctx context.Context, path string, params map[string]string, target protoreflect.ProtoMessage) error {
	reqURL, err := url.Parse(fmt.Sprintf("%s%s", c.baseURL, path))
	if err != nil {
		return err
	}
	q := reqURL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	reqURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bo, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return apiError(resp.StatusCode, bo)
	}

	return proto.Unmarshal(bo, target)
}

func (c *authClient) PostPROTOC(ctx context.Context, path string, body protoreflect.ProtoMessage, target protoreflect.ProtoMessage) error {
	bodyBytes, err := proto.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s%s", c.baseURL, path), bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bo, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return apiError(resp.StatusCode, bo)
	}

	return proto.Unmarshal(bo, target)
}

// apiError builds a readable error from a non-200 response, surfacing the
// server's protobuf error message (e.g. "user already exists") when present.
func apiError(status int, body []byte) error {
	if _, msg := parseErrorBody(body); msg != "" {
		return fmt.Errorf("unexpected status code %d: %s", status, msg)
	}
	return fmt.Errorf("unexpected status code %d", status)
}

func (c *authClient) GetSalt(ctx context.Context, email string) (*GetSaltResponse, error) {
	var resp GetSaltResponse
	err := c.GetPROTOC(ctx, "/users/salt", map[string]string{
		"email": email,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *authClient) signUp(ctx context.Context, signupData *SignupRequest) (bool, error) {
	var resp EmptyResponse
	err := c.PostPROTOC(ctx, "/users/signup", signupData, &resp)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *authClient) SignupEmailResendCode(ctx context.Context, data *SignupEmailResendRequest) (bool, error) {
	var resp EmptyResponse
	err := c.PostPROTOC(ctx, "/users/signup/resend/email", data, &resp)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *authClient) SignupEmailConfirmation(ctx context.Context, data *ConfirmSignupRequest) (bool, error) {
	var resp EmptyResponse
	err := c.PostPROTOC(ctx, "/users/signup/complete/email", data, &resp)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *authClient) LoginPrepare(ctx context.Context, loginData *PrepareRequest) (*PrepareResponse, error) {
	var model PrepareResponse
	err := c.PostPROTOC(ctx, "/users/prepare", loginData, &model)
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (c *authClient) login(ctx context.Context, loginData *LoginRequest) (*LoginResponse, error) {
	pterm.Debug.Printfln("login request is: %v", loginData.String())
	var resp LoginResponse
	err := c.PostPROTOC(ctx, "/users/login", loginData, &resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *authClient) SignOut(ctx context.Context, logoutData *LogoutRequest) (bool, error) {
	var resp EmptyResponse
	err := c.PostPROTOC(ctx, "/users/logout", logoutData, &resp)
	if err != nil {
		return false, err
	}
	return true, nil
}
