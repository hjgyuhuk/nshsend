package send

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"nshsend/core/internal/identity"
	"nshsend/core/internal/receive"
	"nshsend/core/internal/shared"
)

func TestSendAndReceiveMTLSFlow(t *testing.T) {
	senderDir := t.TempDir()
	receiverDir := t.TempDir()
	saveDir := filepath.Join(receiverDir, "downloads")
	srcDir := filepath.Join(senderDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	senderCert, err := identity.GenerateDeviceCertificate()
	if err != nil {
		t.Fatal(err)
	}
	receiverCert, err := identity.GenerateDeviceCertificate()
	if err != nil {
		t.Fatal(err)
	}

	history, err := receive.LoadHistoryStore(receiverDir)
	if err != nil {
		t.Fatal(err)
	}

	receiverSettings := identity.AppSettings{
		Alias:           "Receiver",
		Port:            0,
		HTTPS:           true,
		SaveDir:         saveDir,
		QuickSave:       identity.QuickSaveOn,
		PIN:             "4321",
		VerifyChecksums: true,
		DeviceModel:     "Test",
	}

	var endMu sync.Mutex
	var recvEnded receive.SessionEndEvent
	recvDone := make(chan struct{})

	srv := receive.NewReceiveServer(receiverCert, receiverSettings, history, receive.ServerCallbacks{
		OnSessionEnd: func(end receive.SessionEndEvent) {
			endMu.Lock()
			recvEnded = end
			endMu.Unlock()
			select {
			case <-recvDone:
			default:
				close(recvDone)
			}
		},
	})
	port, err := srv.StartServer(receiverSettings)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer srv.StopServer()

	// Verify mTLS fingerprint pinning rejects wrong expected fingerprint.
	badAddr, _ := shared.ParsePeerAddr("127.0.0.1")
	_, _, err = SendRegisterRequestV2(context.Background(), senderCert, shared.ProtocolHTTPS, badAddr, port, strings.Repeat("A", 64), shared.RegisterRequestV2{
		Alias:       "Sender",
		Version:     shared.ProtocolVersionV2,
		Fingerprint: senderCert.Fingerprint,
		Port:        53317,
		Protocol:    shared.ProtocolHTTPS,
	})
	if err == nil {
		t.Fatalf("expected fingerprint pinning error")
	}

	// Valid registration succeeds and returns receiver cert fingerprint.
	regResp, certFP, err := SendRegisterRequestV2(context.Background(), senderCert, shared.ProtocolHTTPS, badAddr, port, receiverCert.Fingerprint, shared.RegisterRequestV2{
		Alias:       "Sender",
		Version:     shared.ProtocolVersionV2,
		Fingerprint: senderCert.Fingerprint,
		Port:        53317,
		Protocol:    shared.ProtocolHTTPS,
	})
	if err != nil || certFP != receiverCert.Fingerprint || regResp.Alias != "Receiver" {
		t.Fatalf("valid register failed: err=%v certFP=%s resp=%+v", err, certFP, regResp)
	}

	// Write a test binary file.
	payload := bytes.Repeat([]byte{0x42, 0x99, 0x10, 0xfe}, 32*1024)
	srcFile := filepath.Join(srcDir, "archive.bin")
	if err := os.WriteFile(srcFile, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	sendEndCh := make(chan TransferEndEvent, 4)
	coord := NewSendCoordinator(senderCert, CoordinatorCallbacks{
		OnEnd: func(ev TransferEndEvent) {
			sendEndCh <- ev
		},
	})
	senderSettings := identity.AppSettings{
		Alias:       "Sender",
		Port:        53317,
		HTTPS:       true,
		DeviceModel: "Test",
	}
	target := TargetPeerEndpoint{
		Fingerprint: receiverCert.Fingerprint,
		Host:        "127.0.0.1",
		Port:        port,
		Protocol:    shared.ProtocolHTTPS,
	}

	// Wrong PIN must emit pin_required.
	_, err = coord.StartTransfer(senderSettings, target, []StagedItemInput{{Kind: StagedFile, Path: srcFile}}, "")
	if err != nil {
		t.Fatalf("StartTransfer without pin: %v", err)
	}
	select {
	case ev := <-sendEndCh:
		if ev.Status != "pin_required" || !ev.PinRequired {
			t.Fatalf("expected pin_required, got %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for pin_required")
	}

	// Correct PIN completes transfer and saves identical bytes.
	_, err = coord.StartTransfer(senderSettings, target, []StagedItemInput{
		{Kind: StagedFile, Path: srcFile},
		{Kind: StagedText, Text: "Hello from Go"},
	}, "4321")
	if err != nil {
		t.Fatalf("StartTransfer with pin: %v", err)
	}
	select {
	case ev := <-sendEndCh:
		if ev.Status != "finished" {
			t.Fatalf("expected finished, got %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for send finish")
	}

	select {
	case <-recvDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for receiver session end")
	}
	endMu.Lock()
	if recvEnded.Reason != "finished" {
		t.Fatalf("unexpected receiver end: %+v", recvEnded)
	}
	endMu.Unlock()

	savedBytes, err := os.ReadFile(filepath.Join(saveDir, "archive.bin"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.Equal(savedBytes, payload) {
		t.Fatalf("saved file content mismatch")
	}
}

func TestPeerHTTPClientSendsCertDespiteMismatchedAcceptableCAs(t *testing.T) {
	clientCert, err := identity.GenerateDeviceCertificate()
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := identity.GenerateDeviceCertificate()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate LocalSend's CustomClientCertVerifier: mandatory client cert with
	// AcceptableCAs populated by the server's own certificate DN (which does not
	// match the client's self-signed Issuer DN).
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM([]byte(serverCert.CertificatePEM))

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{serverCert.TLSCert},
		ClientAuth:   tls.RequireAnyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS12,
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "missing client cert", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	client := BuildPeerHTTPClient(clientCert, serverCert.Fingerprint, 5*time.Second)
	host := ln.Addr().(*net.TCPAddr).String()
	resp, err := client.Get("https://" + host + "/")
	if err != nil {
		t.Fatalf("expected handshake to succeed despite mismatched AcceptableCAs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}
}

func TestPrepareUploadRetriesOnceOnTransientEOF(t *testing.T) {
	clientCert, err := identity.GenerateDeviceCertificate()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		// First connection: immediately close to trigger EOF.
		conn1, err1 := ln.Accept()
		if err1 == nil {
			_ = conn1.Close()
		}
		// Second connection: read HTTP request and respond 200 OK.
		conn2, err2 := ln.Accept()
		if err2 != nil {
			return
		}
		defer conn2.Close()
		req, errReq := http.ReadRequest(bufio.NewReader(conn2))
		if errReq != nil {
			return
		}
		_ = req.Body.Close()
		body, _ := json.Marshal(shared.PrepareUploadResponseV2{
			SessionID: "retry-sess-1",
			Files:     map[string]string{"f1": "tok1"},
		})
		_, _ = conn2.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " +
			strconv.Itoa(len(body)) + "\r\nConnection: close\r\n\r\n"))
		_, _ = conn2.Write(body)
	}()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	addr, _ := shared.ParsePeerAddr("127.0.0.1")
	client := BuildPeerHTTPClient(clientCert, "", 5*time.Second)

	resp, status, err := SendPrepareUploadV2(context.Background(), client, shared.ProtocolHTTP, addr, uint16(tcpAddr.Port), shared.PrepareUploadRequestV2{
		Info: shared.RegisterRequestV2{
			Alias:       "Sender",
			Version:     shared.ProtocolVersionV2,
			Fingerprint: clientCert.Fingerprint,
			Port:        53317,
			Protocol:    shared.ProtocolHTTP,
		},
		Files: map[string]shared.FileOfferV2{
			"f1": {ID: "f1", FileName: "a.txt", Size: 1, FileType: "text/plain"},
		},
	}, "")
	if err != nil {
		t.Fatalf("expected retry to succeed after transient EOF, got: %v", err)
	}
	if status != http.StatusOK || resp == nil || resp.SessionID != "retry-sess-1" {
		t.Fatalf("unexpected prepare-upload response: status=%d resp=%+v", status, resp)
	}
}
