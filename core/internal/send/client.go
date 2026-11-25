package send

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nshsend/core/internal/identity"
	"nshsend/core/internal/shared"
)

// PeerHTTPStatusError represents a non-2xx HTTP status returned by a LocalSend peer.
type PeerHTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *PeerHTTPStatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("peer returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("peer returned HTTP %d", e.StatusCode)
}

// BuildPeerHTTPClient builds an HTTP/HTTPS client with mTLS client certificate and
// self-signed peer certificate fingerprint pinning.
func BuildPeerHTTPClient(cert *identity.DeviceCertificate, expectedFingerprint string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{cert.TLSCert},
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cert.TLSCert, nil
			},
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				_, err := identity.VerifyPeerSelfSignedCert(rawCerts, expectedFingerprint)
				return err
			},
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func buildV2EndpointURL(protocol shared.ProtocolType, addr shared.PeerAddr, port uint16, path string, query url.Values) string {
	u := url.URL{
		Scheme: string(protocol),
		Host:   fmt.Sprintf("%s:%d", addr.FormatURLHost(), port),
		Path:   "/api/localsend/v2" + path,
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func isTransientEOFError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.HasSuffix(msg, ": eof") || strings.Contains(msg, "connection reset by peer")
}

func executeRequestWithEOFRetry(
	ctx context.Context,
	client *http.Client,
	stage string,
	buildReq func() (*http.Request, error),
) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := buildReq()
		if err != nil {
			return nil, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err == nil {
			log.Printf("[send] %s %s -> HTTP %d (%s)", stage, req.URL.Redacted(), resp.StatusCode, time.Since(start).Round(time.Millisecond))
			return resp, nil
		}
		if attempt == 0 && ctx.Err() == nil && isTransientEOFError(err) {
			log.Printf("[send] %s transient EOF (%v), retrying once", stage, err)
			client.CloseIdleConnections()
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		log.Printf("[send] %s failed (%s): %v", stage, time.Since(start).Round(time.Millisecond), err)
		return nil, err
	}
	return nil, errors.New("unreachable retry state")
}

// SendRegisterRequestV2 sends POST /api/localsend/v2/register and returns the peer response + verified TLS cert fingerprint.
func SendRegisterRequestV2(
	ctx context.Context,
	cert *identity.DeviceCertificate,
	protocol shared.ProtocolType,
	addr shared.PeerAddr,
	port uint16,
	expectedFingerprint string,
	selfInfo shared.RegisterRequestV2,
) (shared.RegisterResponseV2, string, error) {
	client := BuildPeerHTTPClient(cert, expectedFingerprint, 4*time.Second)
	bodyBytes, err := json.Marshal(selfInfo)
	if err != nil {
		return shared.RegisterResponseV2{}, "", err
	}

	endpoint := buildV2EndpointURL(protocol, addr, port, "/register", nil)
	resp, err := executeRequestWithEOFRetry(ctx, client, "register", func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return shared.RegisterResponseV2{}, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return shared.RegisterResponseV2{}, "", decodePeerError(resp)
	}

	var out shared.RegisterResponseV2
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return shared.RegisterResponseV2{}, "", err
	}

	var certFP string
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		certFP = identity.ComputeCertFingerprintDER(resp.TLS.PeerCertificates[0].Raw)
	}
	return out, certFP, nil
}

// SendPrepareUploadV2 sends POST /api/localsend/v2/prepare-upload.
// On HTTP 204 (text-only), returns (nil, 204, nil).
func SendPrepareUploadV2(
	ctx context.Context,
	client *http.Client,
	protocol shared.ProtocolType,
	addr shared.PeerAddr,
	port uint16,
	payload shared.PrepareUploadRequestV2,
	pin string,
) (*shared.PrepareUploadResponseV2, int, error) {
	q := url.Values{}
	if pin != "" {
		q.Set("pin", pin)
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	endpoint := buildV2EndpointURL(protocol, addr, port, "/prepare-upload", q)
	resp, err := executeRequestWithEOFRetry(ctx, client, "prepare-upload", func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, http.StatusNoContent, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, decodePeerError(resp)
	}

	var out shared.PrepareUploadResponseV2
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, http.StatusOK, nil
}

// SendUploadFileV2 streams one file body to POST /api/localsend/v2/upload.
func SendUploadFileV2(
	ctx context.Context,
	client *http.Client,
	protocol shared.ProtocolType,
	addr shared.PeerAddr,
	port uint16,
	sessionID string,
	fileID string,
	token string,
	contentLength int64,
	body io.Reader,
) error {
	q := url.Values{}
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileID)
	q.Set("token", token)

	endpoint := buildV2EndpointURL(protocol, addr, port, "/upload", q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.ContentLength = contentLength

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[send] upload fileId=%s failed (%s): %v", fileID, time.Since(start).Round(time.Millisecond), err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusErr := decodePeerError(resp)
		log.Printf("[send] upload fileId=%s -> %v", fileID, statusErr)
		return statusErr
	}
	log.Printf("[send] upload fileId=%s -> HTTP 200 (%d bytes in %s)", fileID, contentLength, time.Since(start).Round(time.Millisecond))
	return nil
}

// SendCancelSessionV2 sends POST /api/localsend/v2/cancel?sessionId=...
func SendCancelSessionV2(
	ctx context.Context,
	client *http.Client,
	protocol shared.ProtocolType,
	addr shared.PeerAddr,
	port uint16,
	sessionID string,
) error {
	q := url.Values{}
	q.Set("sessionId", sessionID)
	endpoint := buildV2EndpointURL(protocol, addr, port, "/cancel", q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func decodePeerError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var parsed struct {
		Message string `json:"message"`
	}
	msg := ""
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed.Message != "" {
		msg = parsed.Message
	} else if len(raw) > 0 {
		msg = string(raw)
	} else {
		msg = strconv.Itoa(resp.StatusCode)
	}
	return &PeerHTTPStatusError{
		StatusCode: resp.StatusCode,
		Message:    msg,
	}
}

// IsPinRequiredError reports whether err is an HTTP 401 PIN challenge.
func IsPinRequiredError(err error) bool {
	var statusErr *PeerHTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusUnauthorized
}
