package ipc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nshsend/core/internal/discovery"
	"nshsend/core/internal/identity"
	"nshsend/core/internal/receive"
	"nshsend/core/internal/send"
	"nshsend/core/internal/shared"
)

type testNode struct {
	router *Router
	port   uint16
	events chan EventFrame
	resps  chan ResponseFrame
}

func startTestNode(t *testing.T, alias string) *testNode {
	t.Helper()
	dataDir := t.TempDir()
	cert, err := identity.LoadOrCreateDeviceCertificate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	settingsStore, err := identity.LoadSettingsStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	portZero := uint16(0)
	quickOff := identity.QuickSaveOff
	saveDir := filepath.Join(dataDir, "downloads")
	settings, _, err := settingsStore.UpdateSettings(identity.SettingsPatch{
		Alias:     &alias,
		Port:      &portZero,
		QuickSave: &quickOff,
		SaveDir:   &saveDir,
	})
	// Note: UpdateSettings rejects port=0 by design; override directly for ephemeral test port.
	_ = settings
	_ = err

	cfg := settingsStore.CurrentSettings()
	cfg.Alias = alias
	cfg.Port = 0
	cfg.SaveDir = saveDir

	historyStore, err := receive.LoadHistoryStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	pr, pw := io.Pipe()
	writer := NewTransportWriter(pw)
	deviceStore := discovery.NewDeviceStore()

	sendCoord := send.NewSendCoordinator(cert, send.CoordinatorCallbacks{
		OnProgress: func(ev send.TransferProgressEvent) {
			_ = writer.WriteEvent("send.progress", ev)
		},
		OnEnd: func(ev send.TransferEndEvent) {
			_ = writer.WriteEvent("send.ended", ev)
		},
	})

	discCoord := discovery.NewDiscoveryCoordinator(
		deviceStore,
		cert,
		cfg,
		func(ctx context.Context, protocol shared.ProtocolType, addr shared.PeerAddr, port uint16, expectedFP string, selfInfo shared.RegisterRequestV2) (shared.RegisterResponseV2, string, error) {
			return send.SendRegisterRequestV2(ctx, cert, protocol, addr, port, expectedFP, selfInfo)
		},
		func(devices []discovery.PeerDeviceView) {
			_ = writer.WriteEvent("devices.updated", devices)
		},
	)

	recvServer := receive.NewReceiveServer(cert, cfg, historyStore, receive.ServerCallbacks{
		OnRegister: func(addr shared.PeerAddr, info shared.RegisterRequestV2) {
			discCoord.NotifyInboundRegistration(addr, info)
		},
		OnIncomingOffer: func(offer receive.IncomingOfferEvent) {
			_ = writer.WriteEvent("receive.incoming", offer)
		},
		OnOfferAborted: func(sessionID string) {
			_ = writer.WriteEvent("receive.aborted", map[string]string{"sessionId": sessionID})
		},
		OnProgress: func(progress receive.ProgressEvent) {
			_ = writer.WriteEvent("receive.progress", progress)
		},
		OnSessionEnd: func(end receive.SessionEndEvent) {
			_ = writer.WriteEvent("receive.ended", end)
		},
		OnRemoteCancel: func(peerIP string, sessionID string) {
			sendCoord.CancelByRemoteSession(peerIP, sessionID)
		},
		OnHistoryUpdated: func(entries []receive.HistoryEntry) {
			_ = writer.WriteEvent("history.updated", entries)
		},
	})

	router := NewRouter(writer, cert, settingsStore, deviceStore, discCoord, recvServer, sendCoord, historyStore)
	node := &testNode{
		router: router,
		events: make(chan EventFrame, 64),
		resps:  make(chan ResponseFrame, 64),
	}

	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			raw := scanner.Bytes()
			if len(raw) == 0 {
				continue
			}
			var probe struct {
				ID    string `json:"id"`
				Event string `json:"event"`
			}
			_ = json.Unmarshal(raw, &probe)
			if probe.Event != "" {
				var ev EventFrame
				_ = json.Unmarshal(raw, &ev)
				node.events <- ev
			} else if probe.ID != "" {
				var resp ResponseFrame
				_ = json.Unmarshal(raw, &resp)
				node.resps <- resp
			}
		}
	}()

	boundPort, err := recvServer.StartServer(cfg)
	if err != nil {
		_ = pw.Close()
		_ = pr.Close()
		t.Fatalf("StartServer(%s): %v", alias, err)
	}
	node.port = boundPort
	router.UpdateServerState(true, boundPort, cfg.HTTPS, "")

	t.Cleanup(func() {
		recvServer.StopServer()
		_ = pw.Close()
		_ = pr.Close()
	})
	return node
}

func (n *testNode) call(t *testing.T, id, method string, params any) ResponseFrame {
	t.Helper()
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		rawParams = b
	}
	go n.router.DispatchFrame(context.Background(), RequestFrame{
		ID:     id,
		Method: method,
		Params: rawParams,
	})
	for {
		select {
		case resp := <-n.resps:
			if resp.ID == id {
				if resp.Error != nil {
					t.Fatalf("%s error: %+v", method, resp.Error)
				}
				return resp
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for IPC response %s (%s)", id, method)
		}
	}
}

func TestTwoNodeNDJSONEndToEndTransfer(t *testing.T) {
	nodeA := startTestNode(t, "NodeA")
	nodeB := startTestNode(t, "NodeB")

	// Register NodeB from NodeA over mTLS.
	regResp := nodeA.call(t, "1", "discovery.registerManual", map[string]any{
		"host":     "127.0.0.1",
		"port":     nodeB.port,
		"protocol": "https",
	})
	regBytes, _ := json.Marshal(regResp.Result)
	var peerB discovery.PeerDeviceView
	if err := json.Unmarshal(regBytes, &peerB); err != nil {
		t.Fatal(err)
	}
	if peerB.Alias != "NodeB" || !peerB.Verified {
		t.Fatalf("unexpected peerB view: %+v", peerB)
	}

	// Create a 256 KiB random binary file on NodeA's side.
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "random-payload.dat")
	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	nodeA.call(t, "2", "send.start", map[string]any{
		"targetFingerprint": peerB.Fingerprint,
		"host":              "127.0.0.1",
		"port":              nodeB.port,
		"protocol":          "https",
		"items": []map[string]any{
			{"kind": "file", "path": srcFile},
			{"kind": "text", "text": "E2E message"},
		},
	})

	// Wait for receive.incoming on NodeB and accept all files.
	var incoming receive.IncomingOfferEvent
	deadline := time.After(5 * time.Second)
waitIncoming:
	for {
		select {
		case ev := <-nodeB.events:
			if ev.Event == "receive.incoming" {
				b, _ := json.Marshal(ev.Data)
				_ = json.Unmarshal(b, &incoming)
				break waitIncoming
			}
		case <-deadline:
			t.Fatalf("timeout waiting for receive.incoming on NodeB")
		}
	}

	var acceptedIDs []string
	for id := range incoming.Files {
		acceptedIDs = append(acceptedIDs, id)
	}
	nodeB.call(t, "3", "receive.respond", map[string]any{
		"sessionId":       incoming.SessionID,
		"accept":          true,
		"acceptedFileIds": acceptedIDs,
	})

	// Wait for send.ended on NodeA and receive.ended on NodeB.
	waitEvent := func(ch <-chan EventFrame, name string) EventFrame {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case ev := <-ch:
				if ev.Event == name {
					return ev
				}
			case <-timeout:
				t.Fatalf("timeout waiting for %s", name)
			}
		}
	}
	waitEvent(nodeA.events, "send.ended")
	endB := waitEvent(nodeB.events, "receive.ended")
	endBytes, _ := json.Marshal(endB.Data)
	var sessionEnd receive.SessionEndEvent
	_ = json.Unmarshal(endBytes, &sessionEnd)

	receivedBytes, err := os.ReadFile(filepath.Join(sessionEnd.SavedDir, "random-payload.dat"))
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	if !bytes.Equal(receivedBytes, payload) {
		t.Fatalf("received file bytes do not match original payload")
	}

	histResp := nodeB.call(t, "4", "history.list", nil)
	histBytes, _ := json.Marshal(histResp.Result)
	var entries []receive.HistoryEntry
	_ = json.Unmarshal(histBytes, &entries)
	if len(entries) < 2 {
		t.Fatalf("expected >= 2 history entries (file + text), got %d", len(entries))
	}
}
